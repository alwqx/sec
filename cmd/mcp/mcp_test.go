package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/alwqx/sec/aitool"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// connect 用内存管道连接 server 和一个测试客户端。
func connect(t *testing.T) *gomcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := gomcp.NewInMemoryTransports()
	serverSession, err := NewServer(aitool.Default()).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { serverSession.Close() })
	client := gomcp.NewClient(&gomcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { session.Close() })
	return session
}

func text(t *testing.T, result *gomcp.CallToolResult) string {
	t.Helper()
	require.Len(t, result.Content, 1)
	content, ok := result.Content[0].(*gomcp.TextContent)
	require.True(t, ok)
	return content.Text
}

func TestListTools(t *testing.T) {
	session := connect(t)
	require.Contains(t, session.InitializeResult().Instructions, "不给买入")

	result, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := []string{}
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		require.NotEmpty(t, tool.Description)
		require.True(t, tool.Annotations.ReadOnlyHint)
	}
	expected := []string{}
	for _, tool := range aitool.Default().Tools() {
		expected = append(expected, tool.Name)
	}
	require.ElementsMatch(t, expected, names, "SDK 按名字排序列出工具，不保留注册顺序")
}

func TestCallQuote(t *testing.T) {
	defer gock.Off()
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/.*key=600036$").Reply(200).
		BodyString(`var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).
		BodyString(`var hq_str_sh600036="招商银行,40.000,39.000,41.000,42.000,39.500,41.000,41.010,100,4100.000,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-10-30,15:00:00,00,";`)

	session := connect(t)
	result, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "get_quote", Arguments: map[string]any{"codes": []string{"600036"}}})
	require.NoError(t, err)
	require.False(t, result.IsError, text(t, result))

	var out struct {
		SchemaVersion string `json:"schema_version"`
		Tool          string `json:"tool"`
		AsOf          string `json:"as_of"`
		Data          struct {
			Quotes []struct {
				ExCode string  `json:"ex_code"`
				Price  float64 `json:"price"`
			} `json:"quotes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(text(t, result)), &out))
	require.Equal(t, "1", out.SchemaVersion)
	require.Equal(t, "get_quote", out.Tool)
	require.Equal(t, "2026-10-30", out.AsOf)
	require.Equal(t, "SH600036", out.Data.Quotes[0].ExCode)
	require.Equal(t, 41.0, out.Data.Quotes[0].Price)
}

func TestCallInputError(t *testing.T) {
	session := connect(t)
	// 参数错误作为工具结果返回，模型能看到原因，不是协议错误。
	result, err := session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "get_quote", Arguments: map[string]any{"code": "600036"}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Contains(t, text(t, result), "参数格式不正确")

	_, err = session.CallTool(context.Background(), &gomcp.CallToolParams{Name: "no_such_tool", Arguments: map[string]any{}})
	require.Error(t, err)
}

// TestCommand 通过 cobra 命令本身走一遍握手和 tools/list：协议走 cmd 的输入输出，关闭输入后正常退出。
func TestCommand(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	var errOut bytes.Buffer
	cmd := NewMCPCLI()
	cmd.SetIn(inReader)
	cmd.SetOut(outWriter)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{})
	done := make(chan error, 1)
	go func() {
		done <- cmd.ExecuteContext(context.Background())
		outWriter.Close()
	}()

	lines := bufio.NewScanner(outReader)
	request := func(line string) string {
		t.Helper()
		_, err := io.WriteString(inWriter, line+"\n")
		require.NoError(t, err)
		require.True(t, lines.Scan(), "没有收到响应")
		return lines.Text()
	}
	request(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	_, err := io.WriteString(inWriter, `{"jsonrpc":"2.0","method":"notifications/initialized"}`+"\n")
	require.NoError(t, err)
	var list struct {
		ID     int `json:"id"`
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal([]byte(request(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)), &list))
	require.Equal(t, 2, list.ID)
	require.Equal(t, "get_quote", list.Result.Tools[0].Name)

	require.NoError(t, inWriter.Close())
	require.NoError(t, <-done)
	require.Empty(t, errOut.String(), "正常运行时 stderr 没有日志")
}
