// Package mcp 实现 sec mcp：通过 stdio 提供只读的 MCP server，让 Claude Code、Cursor 等外部 agent 调用 aitool 里的工具。
// 模型由外部 agent 提供，sec 只提供工具，不读模型配置。设计见 docs/ai.md 和 docs/mcp.md。
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/version"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// toolTimeout 是单次工具调用的超时，和 Desktop 美股接口一致。
const toolTimeout = 30 * time.Second

// instructions 在连接时交给模型，说明使用这些工具的规则。
const instructions = `sec 提供 A 股、港股、美股、基金和宏观数据的只读工具。
回答时只使用工具返回的数字，工具没有给出的数值就说数据中没有；保留 notes 和 disclaimer 里的口径说明；
写明 as_of 或各条数据自带的日期，不同日期的数据要分别说明；null 表示缺失，不能当作 0；
只解释数据，不给买入、卖出、持有或目标价等投资建议。`

func NewMCPCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run a read-only MCP server over stdio for AI agents such as Claude Code",
		Long: "Run a read-only MCP server over stdio. stdout carries the MCP protocol and logs go to stderr.\n" +
			"Add it to an agent with, for example: claude mcp add sec -- sec mcp",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// stdout 只给协议用，日志一律写 stderr。
			level := slog.LevelWarn
			if debug, _ := cmd.Flags().GetBool("debug"); debug {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level})))
			transport := &gomcp.IOTransport{Reader: io.NopCloser(cmd.InOrStdin()), Writer: nopWriteCloser{cmd.OutOrStdout()}}
			return NewServer(aitool.Default()).Run(cmd.Context(), transport)
		},
	}
	cmd.Flags().BoolP("debug", "D", false, "Write debug logs to stderr")
	return cmd
}

// NewServer 把注册表里的工具都注册到一个 MCP server 上。
func NewServer(registry *aitool.Registry) *gomcp.Server {
	serverVersion := version.Version
	if serverVersion == "" {
		serverVersion = "dev"
	}
	server := gomcp.NewServer(&gomcp.Implementation{Name: "sec", Version: serverVersion}, &gomcp.ServerOptions{Instructions: instructions})
	openWorld := true
	for _, tool := range registry.Tools() {
		server.AddTool(&gomcp.Tool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			// 只读、查询外部数据源。
			Annotations: &gomcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld},
		}, handler(tool))
	}
	return server
}

// handler 调用工具，把结果编码成 JSON 文本。工具出错时返回 IsError 的结果而不是协议错误，让模型看到原因并调整参数。
func handler(tool aitool.Tool) gomcp.ToolHandler {
	return func(ctx context.Context, req *gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
		ctx, cancel := context.WithTimeout(ctx, toolTimeout)
		defer cancel()
		result, err := tool.Call(ctx, req.Params.Arguments)
		if err != nil {
			if !aitool.IsInputError(err) {
				slog.ErrorContext(ctx, "tool call failed", "tool", tool.Name, "error", err)
			}
			return errorResult(err.Error()), nil
		}
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(result); err != nil {
			slog.ErrorContext(ctx, "encode tool result", "tool", tool.Name, "error", err)
			return errorResult("工具结果编码失败"), nil
		}
		return &gomcp.CallToolResult{Content: []gomcp.Content{&gomcp.TextContent{Text: buf.String()}}}, nil
	}
}

// nopWriteCloser 让 cmd.OutOrStdout() 满足 IOTransport 的 io.WriteCloser；关闭连接时不关闭 stdout。
type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func errorResult(text string) *gomcp.CallToolResult {
	return &gomcp.CallToolResult{IsError: true, Content: []gomcp.Content{&gomcp.TextContent{Text: text}}}
}
