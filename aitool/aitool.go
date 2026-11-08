// Package aitool 是给大模型使用的只读工具注册表，sec mcp、sec ask 和 Desktop 对话共用。
//
// 工具只取数、不写入：不改自选、不写配置、不下载文件。所有数值都由现有的 provider 和计算包得出，
// 输出统一包在 Result 里，带来源、截止日期和口径说明，模型只负责选择工具和解释结果。
// 本包不依赖 cmd/、desktop/ 和任何模型或 MCP SDK，可以在所有平台上编译和测试。设计见 docs/ai.md。
package aitool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// SchemaVersion 是 Result 的格式版本。删除或重命名字段、改变含义时加 1，新增字段不变。
const SchemaVersion = "1"

// Tool 是一个只读工具，参数和返回都是 JSON。
type Tool struct {
	// Name 是英文 snake_case 名字，如 get_quote，在注册表里唯一。
	Name string
	// Description 是给模型看的中文说明：做什么、什么时候用、返回什么。
	Description string
	// InputSchema 是参数的 JSON Schema，顶层必须是 object。MCP 和模型 tool use 共用。
	InputSchema json.RawMessage
	// Call 执行工具。参数不合法时返回 InputError，上游失败返回其他 error；error 文本会原样交给模型。
	Call func(ctx context.Context, input json.RawMessage) (*Result, error)
}

// Result 是所有工具统一的输出信封。
type Result struct {
	SchemaVersion string `json:"schema_version"`
	Tool          string `json:"tool"`
	// AsOf 是数据本身的截止日期（交易日、报告期或统计期），不是查询时间。
	// 结果里的数据不是同一个时点时为空，各自的日期在 Data 里。
	AsOf string `json:"as_of,omitempty"`
	// Source 是数据来源，比如「新浪财经行情」。
	Source string `json:"source"`
	// Notes 是口径说明、推导说明和截短说明，回答时应保留。没有时是空数组，不是 null。
	Notes []string `json:"notes"`
	// Disclaimer 是结果自带的免责声明，比如估值的「不构成投资建议」。
	Disclaimer string `json:"disclaimer,omitempty"`
	Data       any    `json:"data"`
}

// NewResult 返回填好版本号、工具名和空 Notes 的结果。
func NewResult(tool, source string, data any) *Result {
	return &Result{SchemaVersion: SchemaVersion, Tool: tool, Source: source, Notes: []string{}, Data: data}
}

// InputError 表示工具参数不合法，调用方应提示模型修改参数后重试，而不是当作上游故障。
type InputError struct{ msg string }

func (e *InputError) Error() string { return e.msg }

// Inputf 返回带格式化说明的 InputError。
func Inputf(format string, args ...any) error {
	return &InputError{msg: fmt.Sprintf(format, args...)}
}

// IsInputError 判断 err 是否为参数错误。
func IsInputError(err error) bool {
	var inputErr *InputError
	return errors.As(err, &inputErr)
}

// Decode 把参数严格解码到 v：不认识的字段、多余的内容都算参数错误。空参数按 {} 处理。
// 只检查 JSON 结构和类型，必填项和取值范围由各工具自己检查。
func Decode(input json.RawMessage, v any) error {
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return Inputf("参数格式不正确：%v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Inputf("参数格式不正确：JSON 之后还有多余内容")
	}
	return nil
}

// toolName 和 MCP 对工具名的要求一致，并且只用小写，便于模型记忆。
var toolName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Registry 按名字查找工具，顺序和注册顺序一致，便于测试和生成文档。
type Registry struct {
	tools  []Tool
	byName map[string]int
}

// NewRegistry 检查并注册工具：名字合法且不重复、有说明、参数 Schema 顶层是 object、有 Call。
func NewRegistry(tools ...Tool) (*Registry, error) {
	r := &Registry{byName: make(map[string]int, len(tools))}
	for _, tool := range tools {
		if !toolName.MatchString(tool.Name) {
			return nil, fmt.Errorf("工具名不合法：%q", tool.Name)
		}
		if _, ok := r.byName[tool.Name]; ok {
			return nil, fmt.Errorf("工具名重复：%s", tool.Name)
		}
		if tool.Description == "" || tool.Call == nil {
			return nil, fmt.Errorf("工具 %s 缺少说明或实现", tool.Name)
		}
		var schema struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema.Type != "object" {
			return nil, fmt.Errorf("工具 %s 的参数 Schema 顶层必须是 object", tool.Name)
		}
		r.byName[tool.Name] = len(r.tools)
		r.tools = append(r.tools, tool)
	}
	return r, nil
}

// Tools 返回全部工具，按注册顺序。
func (r *Registry) Tools() []Tool {
	return append([]Tool(nil), r.tools...)
}

// Lookup 按名字查找工具。
func (r *Registry) Lookup(name string) (Tool, bool) {
	i, ok := r.byName[name]
	if !ok {
		return Tool{}, false
	}
	return r.tools[i], true
}

// Default 返回 sec 提供的全部工具。新工具在这里注册。
// 宏观命令在 cmd/macro 里，本包不能依赖，由调用方传入 macro；为 nil 时不注册 get_macro_series。
func Default(macro MacroSource) *Registry {
	tools := []Tool{
		SearchTool(),
		QuoteTool(),
		KlineSummaryTool(),
		FinancialsTool(),
		ValuationTool(),
		FilingsTool(),
	}
	if macro != nil {
		tools = append(tools, MacroSeriesTool(macro))
	}
	r, err := NewRegistry(tools...)
	if err != nil {
		// 工具在代码里写死，出错是编程错误，测试会覆盖到。
		panic(err)
	}
	return r
}
