package llm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/stretchr/testify/require"
)

func TestAnthropic(t *testing.T) {
	rec := &recorder{t: t, replies: []string{
		`{"model":"test-model-1","stop_reason":"tool_use","usage":{"input_tokens":12,"output_tokens":34},"content":[
			{"type":"thinking","thinking":"","signature":"sig-abc"},
			{"type":"text","text":"我查一下。"},
			{"type":"tool_use","id":"toolu_1","name":"get_quote","input":{"codes":["600519"]}}]}`,
		`{"model":"test-model-1","stop_reason":"end_turn","usage":{"input_tokens":50,"output_tokens":20},"content":[{"type":"text","text":"贵州茅台最新价 1500 元。"}]}`,
	}}
	server := rec.serve()
	client, err := New(custom(config.AIAPIAnthropic, server.URL+"/"), "sk-ant-test")
	require.NoError(t, err)

	req := Request{System: "只用工具给出的数字", Tools: []Tool{quoteTool}, Messages: []Message{{Role: RoleUser, Text: "茅台多少钱？"}}}
	resp, err := client.Chat(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "/v1/messages", rec.paths[0], "结尾的斜杠不重复")
	h := rec.headers[0]
	require.Equal(t, "sk-ant-test", h.Get("x-api-key"))
	require.Equal(t, anthropicVersion, h.Get("anthropic-version"))
	require.Equal(t, "application/json", h.Get("Content-Type"))
	require.Contains(t, h.Get("User-Agent"), "sec/")
	require.Empty(t, h.Get("Authorization"))

	body := rec.bodies[0]
	require.Equal(t, "test-model", body["model"])
	require.EqualValues(t, DefaultMaxTokens, body["max_tokens"])
	require.Equal(t, "只用工具给出的数字", body["system"])
	for _, field := range []string{"temperature", "top_p", "top_k"} {
		require.NotContains(t, body, field, "2026-10-21 版本不接受采样参数")
	}
	tool := body["tools"].([]any)[0].(map[string]any)
	require.Equal(t, "get_quote", tool["name"])
	require.Contains(t, tool, "input_schema")
	require.Equal(t, []any{map[string]any{"type": "text", "text": "茅台多少钱？"}}, body["messages"].([]any)[0].(map[string]any)["content"])

	require.Equal(t, StopToolUse, resp.StopReason)
	require.Equal(t, "tool_use", resp.RawStopReason)
	require.Equal(t, "test-model-1", resp.Model)
	require.Equal(t, Usage{InputTokens: 12, OutputTokens: 34}, resp.Usage)
	require.Equal(t, "我查一下。", resp.Message.Text)
	require.Equal(t, []ToolCall{{ID: "toolu_1", Name: "get_quote", Input: json.RawMessage(`{"codes":["600519"]}`)}}, resp.Message.ToolCalls)

	// 第二轮：助手消息原样发回（含 thinking 块和签名），工具结果放在用户消息里。
	req.Messages = append(req.Messages, resp.Message, Message{Role: RoleUser, ToolResults: []ToolResult{
		{CallID: "toolu_1", Content: `{"price":1500}`}, {CallID: "toolu_2", Content: "参数错误", IsError: true},
	}})
	req.MaxTokens = 2048
	resp, err = client.Chat(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, StopEnd, resp.StopReason)
	require.Equal(t, "贵州茅台最新价 1500 元。", resp.Message.Text)
	require.Empty(t, resp.Message.ToolCalls)

	messages := rec.bodies[1]["messages"].([]any)
	require.EqualValues(t, 2048, rec.bodies[1]["max_tokens"])
	assistant := messages[1].(map[string]any)
	require.Equal(t, "assistant", assistant["role"])
	content := assistant["content"].([]any)
	require.Len(t, content, 3)
	require.Equal(t, map[string]any{"type": "thinking", "thinking": "", "signature": "sig-abc"}, content[0], "thinking 块原样发回")
	results := messages[2].(map[string]any)["content"].([]any)
	require.Equal(t, map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": `{"price":1500}`}, results[0])
	require.Equal(t, map[string]any{"type": "tool_result", "tool_use_id": "toolu_2", "content": "参数错误", "is_error": true}, results[1])
}

func TestAnthropicStopReasons(t *testing.T) {
	for raw, want := range map[string]string{"end_turn": StopEnd, "stop_sequence": StopEnd, "max_tokens": StopMaxTokens, "tool_use": StopToolUse, "refusal": StopRefusal, "pause_turn": StopOther} {
		require.Equal(t, want, anthropicStopReason(raw), raw)
	}
}

func TestAnthropicLocalWithoutKey(t *testing.T) {
	rec := &recorder{t: t, replies: []string{`{"stop_reason":"end_turn","content":[{"type":"text","text":"hi"}]}`}}
	server := rec.serve()
	client, err := New(custom(config.AIAPIAnthropic, server.URL), "")
	require.NoError(t, err, "本机的 Anthropic 兼容接口可以不用 Key")
	_, err = client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	require.NoError(t, err)
	require.Empty(t, rec.headers[0].Get("x-api-key"), "没有 Key 时不发")
	_, err = New(custom(config.AIAPIAnthropic, "https://gw.example.com"), "")
	require.NoError(t, err, "自定义厂商（自建网关）可以不用 Key")
	for _, provider := range []string{"anthropic", "deepseek", "openai"} {
		_, err = New(config.AIProfile{Provider: provider, Model: "m"}, "")
		require.ErrorIs(t, err, ErrMissingKey, "远程厂商没有 Key 时直接报错：%s", provider)
	}
}

func TestAnthropicResend(t *testing.T) {
	rec := &recorder{t: t, replies: []string{
		`{"stop_reason":"tool_use","content":[{"type":"thinking","thinking":"","signature":"sig"},{"type":"text","text":"查一下"},
			{"type":"tool_use","id":"t1","name":"get_quote","input":{"code":"1"}},{"type":"tool_use","id":"t2","name":"nosuch","input":{}}]}`,
		`{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`,
		`{"stop_reason":"max_tokens","content":[]}`,
	}}
	server := rec.serve()
	client, err := New(custom(config.AIAPIAnthropic, server.URL), "k")
	require.NoError(t, err)
	req := Request{Messages: []Message{{Role: RoleUser, Text: "q"}}}
	resp, err := client.Chat(context.Background(), req)
	require.NoError(t, err)

	// 调用方去掉了一个未知工具的调用：按改过的内容重新拼装，不再带 thinking 块，也不会有对不上的 tool_use。
	edited := resp.Message
	edited.ToolCalls = edited.ToolCalls[:1]
	req.Messages = append(req.Messages, edited, Message{Role: RoleUser, ToolResults: []ToolResult{{CallID: "t1", Content: "1"}}})
	_, err = client.Chat(context.Background(), req)
	require.NoError(t, err)
	content := rec.bodies[1]["messages"].([]any)[1].(map[string]any)["content"].([]any)
	require.Equal(t, []any{
		map[string]any{"type": "text", "text": "查一下"},
		map[string]any{"type": "tool_use", "id": "t1", "name": "get_quote", "input": map[string]any{"code": "1"}},
	}, content)

	// 截断时 content 为空：不保存原始内容，原样追加的空消息在本地报错，不发给厂商。
	resp, err = client.Chat(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, StopMaxTokens, resp.StopReason)
	require.Empty(t, resp.Message.raw)
	_, err = client.Chat(context.Background(), Request{Messages: append(req.Messages, resp.Message)})
	require.ErrorContains(t, err, "第 4 条消息（assistant）是空的")
	require.Len(t, rec.bodies, 3, "空消息不发请求")
}

func TestCrossProtocolToolCalls(t *testing.T) {
	// OpenAI 兼容接口返回的不合法参数：发回 Anthropic 时是空对象，发回 OpenAI 兼容接口时还原成原来的字符串。
	call := ToolCall{ID: "c1", Name: "get_quote", Input: toolInput("{bad")}
	content, _, err := (&anthropicClient{}).content(Message{Role: RoleAssistant, ToolCalls: []ToolCall{call}})
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"tool_use","id":"c1","name":"get_quote","input":{}}]`, string(content))
	client := &openAIClient{provider: "deepseek"}
	msgs := client.openAIMessages(Message{Role: RoleAssistant, ToolCalls: []ToolCall{call}})
	require.Equal(t, "{bad", msgs[0].ToolCalls[0].Function.Arguments)
	require.Equal(t, `{"a":1}`, toolArguments(json.RawMessage(`{"a":1}`)))
	require.Equal(t, "{}", toolArguments(nil))
}

func TestAnthropicRawPerProvider(t *testing.T) {
	// 只有 thinking 块的回复（如被截断）可以原样追加继续；换到别的厂商的 Anthropic 兼容接口时不发原始内容。
	rec := &recorder{t: t, replies: []string{
		`{"stop_reason":"max_tokens","content":[{"type":"thinking","thinking":"","signature":"sig"}]}`,
		`{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`,
		`{"stop_reason":"tool_use","content":[{"type":"thinking","thinking":"","signature":"sig2"},{"type":"tool_use","id":"t1","name":"get_quote","input":{}}]}`,
		`{"stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`,
	}}
	server := rec.serve()
	claude, err := New(custom(config.AIAPIAnthropic, server.URL), "k")
	require.NoError(t, err)
	req := Request{Messages: []Message{{Role: RoleUser, Text: "q"}}}
	resp, err := claude.Chat(context.Background(), req)
	require.NoError(t, err)
	req.Messages = append(req.Messages, resp.Message, Message{Role: RoleUser, Text: "继续"})
	_, err = claude.Chat(context.Background(), req)
	require.NoError(t, err, "只有原始内容的助手消息不算空")
	content := rec.bodies[1]["messages"].([]any)[1].(map[string]any)["content"].([]any)
	require.Equal(t, "thinking", content[0].(map[string]any)["type"])

	resp, err = claude.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "q"}}})
	require.NoError(t, err)
	ds, err := New(config.AIProfile{Provider: "deepseek", API: config.AIAPIAnthropic, BaseURL: server.URL + "/anthropic", Model: "m"}, "k")
	require.NoError(t, err)
	_, err = ds.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "q"}, resp.Message, {Role: RoleUser, ToolResults: []ToolResult{{CallID: "t1", Content: "1"}}}}})
	require.NoError(t, err)
	content = rec.bodies[3]["messages"].([]any)[1].(map[string]any)["content"].([]any)
	require.Equal(t, []any{map[string]any{"type": "tool_use", "id": "t1", "name": "get_quote", "input": map[string]any{}}}, content, "换厂商时不带别家签名的 thinking 块")

	// 别的厂商的只有 thinking 的消息，重新拼装后是空的：在本地报错。
	only := Message{Role: RoleAssistant, raw: json.RawMessage(`[{"type":"thinking"}]`), rawProto: protoAnthropic, rawProvider: "anthropic"}
	_, err = ds.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "q"}, only}})
	require.ErrorContains(t, err, "第 2 条消息（assistant）是空的")
}

func TestAnthropicContent(t *testing.T) {
	client := &anthropicClient{provider: "anthropic"}
	raw := json.RawMessage(`[{"type":"thinking","thinking":"","signature":"s"},{"type":"text","text":"hi"}]`)
	fromSame := Message{Role: RoleAssistant, Text: "hi", raw: raw, rawProto: protoAnthropic, rawProvider: "anthropic"}
	fromSame.rawSig = signature(fromSame)
	edited := fromSame
	edited.Text = "改过"
	fromOther := fromSame
	fromOther.rawProvider = "deepseek"

	for name, c := range map[string]struct {
		msg   Message
		want  string
		empty bool
	}{
		"用户文字": {Message{Role: RoleUser, Text: "q"}, `[{"type":"text","text":"q"}]`, false},
		"工具结果在文字之前": {Message{Role: RoleUser, Text: "再问", ToolResults: []ToolResult{{CallID: "t1", Content: "ok"}, {CallID: "t2", Content: "坏了", IsError: true}}},
			`[{"type":"tool_result","tool_use_id":"t1","content":"ok"},{"type":"tool_result","tool_use_id":"t2","content":"坏了","is_error":true},{"type":"text","text":"再问"}]`, false},
		"助手文字和调用": {Message{Role: RoleAssistant, Text: "查", ToolCalls: []ToolCall{{ID: "t1", Name: "get_quote", Input: json.RawMessage(`{"a":1}`)}, {ID: "t2", Name: "x"}}},
			`[{"type":"text","text":"查"},{"type":"tool_use","id":"t1","name":"get_quote","input":{"a":1}},{"type":"tool_use","id":"t2","name":"x","input":{}}]`, false},
		"同一厂商原样发回":   {fromSame, string(raw), false},
		"改过后重新拼装":    {edited, `[{"type":"text","text":"改过"}]`, false},
		"别的厂商重新拼装":   {fromOther, `[{"type":"text","text":"hi"}]`, false},
		"空消息":        {Message{Role: RoleUser}, `[]`, true},
		"用户消息不用原始内容": {Message{Role: RoleUser, Text: "q", raw: raw, rawProto: protoAnthropic, rawProvider: "anthropic"}, `[{"type":"text","text":"q"}]`, false},
	} {
		got, empty, err := client.content(c.msg)
		require.NoError(t, err, name)
		require.JSONEq(t, c.want, string(got), name)
		require.Equal(t, c.empty, empty, name)
	}
}

func TestObjectOrEmpty(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1}`: `{"a":1}`, ` {"a":1}`: ` {"a":1}`, ``: `{}`, `"{bad"`: `{}`, `[1]`: `{}`, `null`: `{}`,
	} {
		require.Equal(t, want, string(objectOrEmpty(json.RawMessage(in))), in)
	}
}

func TestParseAnthropicError(t *testing.T) {
	for payload, want := range map[string][2]string{
		`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`: {"overloaded_error", "Overloaded"},
		`{"type":"error","error":{"message":"only message"}}`:                         {"", "only message"},
		`{}`:                     {"", ""},
		`<html>502</html>`:       {"", ""},
		`{"error":"not object"}`: {"", ""},
	} {
		kind, message := parseAnthropicError([]byte(payload))
		require.Equal(t, want, [2]string{kind, message}, payload)
	}
}

func TestAnthropicChatErrors(t *testing.T) {
	rec := &recorder{t: t, replies: []string{`{"stop_reason":"end_turn","content":"not blocks"}`}}
	server := rec.serve()
	client, err := New(custom(config.AIAPIAnthropic, server.URL), "k")
	require.NoError(t, err)
	_, err = client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "q"}}})
	require.Error(t, err, "content 不是内容块数组")

	_, err = client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser}}})
	require.ErrorContains(t, err, "第 1 条消息（user）是空的", "空消息不发请求")
	require.Len(t, rec.bodies, 1)

	// 没有内容块的回复（content 缺失或为 null）：文字为空，不保存原始内容。
	rec2 := &recorder{t: t, replies: []string{`{"stop_reason":"refusal"}`, `{"stop_reason":"refusal","content":null}`}}
	server2 := rec2.serve()
	client, err = New(custom(config.AIAPIAnthropic, server2.URL), "k")
	require.NoError(t, err)
	for range 2 {
		resp, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "q"}}})
		require.NoError(t, err)
		require.Equal(t, StopRefusal, resp.StopReason)
		require.Empty(t, resp.Message.Text)
		require.Empty(t, resp.Message.raw)
	}
}
