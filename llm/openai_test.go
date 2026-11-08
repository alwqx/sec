package llm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAI(t *testing.T) {
	rec := &recorder{t: t, replies: []string{
		`{"model":"deepseek-chat","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"想一想","tool_calls":[
			{"id":"call_a","type":"function","function":{"name":"get_quote","arguments":"{\"codes\":[\"600519\"]}"}},
			{"id":"call_b","type":"function","function":{"name":"get_quote","arguments":""}},
			{"id":"call_c","type":"function","function":{"name":"get_quote","arguments":"{bad"}}]}}],
			"usage":{"prompt_tokens":7,"completion_tokens":9}}`,
		`{"model":"deepseek-chat","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"1500 元"}}]}`,
	}}
	server := rec.serve()
	profile := config.AIProfile{Provider: "deepseek", BaseURL: server.URL, Model: "deepseek-chat"}
	client, err := New(profile, "sk-ds")
	require.NoError(t, err)

	req := Request{System: "规则", Tools: []Tool{quoteTool}, Messages: []Message{{Role: RoleUser, Text: "茅台多少钱？"}}}
	resp, err := client.Chat(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "/chat/completions", rec.paths[0])
	require.Equal(t, "Bearer sk-ds", rec.headers[0].Get("Authorization"))
	body := rec.bodies[0]
	require.EqualValues(t, DefaultMaxTokens, body["max_tokens"], "OpenAI 以外的兼容接口用 max_tokens")
	require.NotContains(t, body, "max_completion_tokens")
	messages := body["messages"].([]any)
	require.Equal(t, map[string]any{"role": "system", "content": "规则"}, messages[0])
	require.Equal(t, map[string]any{"role": "user", "content": "茅台多少钱？"}, messages[1])
	tool := body["tools"].([]any)[0].(map[string]any)
	require.Equal(t, "function", tool["type"])
	require.Contains(t, tool["function"], "parameters")

	require.Equal(t, StopToolUse, resp.StopReason)
	require.Equal(t, Usage{InputTokens: 7, OutputTokens: 9}, resp.Usage)
	require.Empty(t, resp.Message.Text)
	calls := resp.Message.ToolCalls
	require.Len(t, calls, 3)
	require.JSONEq(t, `{"codes":["600519"]}`, string(calls[0].Input))
	require.JSONEq(t, `{}`, string(calls[1].Input), "空参数当作没有参数")
	require.Equal(t, `"{bad"`, string(calls[2].Input), "不合法的参数保存成字符串，交给参数校验报错")

	// 第二轮：助手消息的 content 是 null，每个工具结果一条 tool 消息；推理内容发回给 DeepSeek。
	req.Messages = append(req.Messages, resp.Message, Message{Role: RoleUser, ToolResults: []ToolResult{
		{CallID: "call_a", Content: `{"price":1500}`}, {CallID: "call_c", Content: "参数格式不正确", IsError: true},
	}})
	resp, err = client.Chat(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, StopEnd, resp.StopReason)
	require.Equal(t, "1500 元", resp.Message.Text)
	messages = rec.bodies[1]["messages"].([]any)
	assistant := messages[2].(map[string]any)
	require.Nil(t, assistant["content"])
	require.Equal(t, "想一想", assistant["reasoning_content"], "DeepSeek 的推理内容原样发回")
	require.Equal(t, "call_a", assistant["tool_calls"].([]any)[0].(map[string]any)["id"])
	require.Equal(t, map[string]any{"role": "tool", "tool_call_id": "call_a", "content": `{"price":1500}`}, messages[3])
	require.Equal(t, "工具调用出错：参数格式不正确", messages[4].(map[string]any)["content"])
	require.Len(t, messages, 5, "只有工具结果时不加空的用户消息")
}

func TestOpenAIProviders(t *testing.T) {
	// OpenAI 用 max_completion_tokens；本机 Ollama 不给 id、调用工具时 finish_reason 是 stop，也不发 Authorization。
	rec := &recorder{t: t, replies: []string{
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"hi"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"","tool_calls":[{"type":"function","function":{"name":"get_quote","arguments":"{}"}}]}}]}`,
	}}
	server := rec.serve()
	client, err := New(config.AIProfile{Provider: "openai", BaseURL: server.URL + "/v1", Model: "gpt"}, "sk-oa")
	require.NoError(t, err)
	_, err = client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}, MaxTokens: 100})
	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", rec.paths[0])
	require.EqualValues(t, 100, rec.bodies[0]["max_completion_tokens"])
	require.NotContains(t, rec.bodies[0], "max_tokens")
	require.NotContains(t, rec.bodies[0], "tools", "没有工具时不发")

	local, err := New(config.AIProfile{Provider: "ollama", BaseURL: server.URL, Model: "qwen3"}, "")
	require.NoError(t, err)
	resp, err := local.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	require.NoError(t, err)
	require.Empty(t, rec.headers[1].Get("Authorization"), "没有 Key 时不发")
	require.Equal(t, StopToolUse, resp.StopReason, "有工具调用就按 tool_use 处理")
	require.Regexp(t, `^call_[0-9a-f]{16}$`, resp.Message.ToolCalls[0].ID, "没有 id 时随机生成，不同轮不会重名")

	for raw, want := range map[string]string{"stop": StopEnd, "length": StopMaxTokens, "tool_calls": StopToolUse, "content_filter": StopRefusal, "weird": StopOther} {
		require.Equal(t, want, openAIStopReason(raw), raw)
	}
}

func TestOpenAIReasoning(t *testing.T) {
	// DeepSeek 带工具时要求发回之前所有助手消息的 reasoning_content；只发回给产生它的厂商。
	rec := &recorder{t: t, replies: []string{
		`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"reasoning_content":"先查行情","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_quote","arguments":"{}"}}]}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"1500","reasoning_content":"看结果"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`,
		`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`,
	}}
	server := rec.serve()
	ds, err := New(config.AIProfile{Provider: "deepseek", BaseURL: server.URL, Model: "m"}, "k")
	require.NoError(t, err)
	req := Request{Tools: []Tool{quoteTool}, Messages: []Message{{Role: RoleUser, Text: "q"}}}
	resp, err := ds.Chat(context.Background(), req)
	require.NoError(t, err)
	req.Messages = append(req.Messages, resp.Message, Message{Role: RoleUser, ToolResults: []ToolResult{{CallID: "c1", Content: "1500"}}})
	resp, err = ds.Chat(context.Background(), req)
	require.NoError(t, err)
	req.Messages = append(req.Messages, resp.Message, Message{Role: RoleUser, Text: "再问"})
	_, err = ds.Chat(context.Background(), req)
	require.NoError(t, err)
	messages := rec.bodies[2]["messages"].([]any)
	require.Equal(t, "先查行情", messages[1].(map[string]any)["reasoning_content"], "带工具调用的助手消息")
	require.Equal(t, "看结果", messages[3].(map[string]any)["reasoning_content"], "最终回答的助手消息也发回")

	other, err := New(config.AIProfile{Provider: "qwen", BaseURL: server.URL, Model: "m"}, "k")
	require.NoError(t, err)
	_, err = other.Chat(context.Background(), req)
	require.NoError(t, err)
	for _, m := range rec.bodies[3]["messages"].([]any) {
		require.NotContains(t, m, "reasoning_content", "不发给别的厂商")
	}
}

func TestReplyText(t *testing.T) {
	for raw, want := range map[string]string{
		`"hi"`: "hi", `null`: "", ``: "",
		`[{"type":"text","text":"a"},{"type":"image_url"},{"type":"text","text":"b"}]`: "ab",
	} {
		got, err := replyText(json.RawMessage(raw))
		require.NoError(t, err, raw)
		require.Equal(t, want, got, raw)
	}
	_, err := replyText(json.RawMessage(`123`))
	require.Error(t, err)

	rec := &recorder{t: t, replies: []string{`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[{"type":"text","text":"数组"}]}}]}`}}
	server := rec.serve()
	client, err := New(custom(config.AIAPIOpenAI, server.URL), "")
	require.NoError(t, err)
	resp, err := client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	require.NoError(t, err, "content 是数组时也能解析")
	require.Equal(t, "数组", resp.Message.Text)
}

func TestOpenAIMaxTokensField(t *testing.T) {
	for provider, want := range map[string]string{
		"openai": "max_completion_tokens", "deepseek": "max_tokens", "qwen": "max_tokens", "moonshot": "max_tokens",
		"zhipu": "max_tokens", "ollama": "max_tokens", config.AIProviderCustom: "max_tokens",
	} {
		require.Equal(t, want, (&openAIClient{provider: provider}).maxTokensField(), provider)
	}
}

func TestOpenAIMessagesConversion(t *testing.T) {
	ds := &openAIClient{provider: "deepseek"}
	encode := func(msgs []openAIMessage) string {
		data, err := json.Marshal(msgs)
		require.NoError(t, err)
		return string(data)
	}
	for name, c := range map[string]struct {
		msg  Message
		want string
	}{
		"用户文字": {Message{Role: RoleUser, Text: "q"}, `[{"role":"user","content":"q"}]`},
		"工具结果在用户文字之前，出错的加说明": {Message{Role: RoleUser, Text: "再问", ToolResults: []ToolResult{{CallID: "c1", Content: "ok"}, {CallID: "c2", Content: "坏了", IsError: true}}},
			`[{"role":"tool","content":"ok","tool_call_id":"c1"},{"role":"tool","content":"工具调用出错：坏了","tool_call_id":"c2"},{"role":"user","content":"再问"}]`},
		"只有工具结果时不加空的用户消息": {Message{Role: RoleUser, ToolResults: []ToolResult{{CallID: "c1", Content: "ok"}}},
			`[{"role":"tool","content":"ok","tool_call_id":"c1"}]`},
		"助手文字": {Message{Role: RoleAssistant, Text: "答"}, `[{"role":"assistant","content":"答"}]`},
		"只有工具调用时 content 是 null": {Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "get_quote", Input: json.RawMessage(`{"a":1}`)}}},
			`[{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_quote","arguments":"{\"a\":1}"}}]}]`},
		"同一厂商的推理内容发回": {Message{Role: RoleAssistant, Text: "答", reasoning: "想", reasoningProvider: "deepseek"},
			`[{"role":"assistant","content":"答","reasoning_content":"想"}]`},
		"别的厂商的推理内容不发": {Message{Role: RoleAssistant, Text: "答", reasoning: "想", reasoningProvider: "moonshot"},
			`[{"role":"assistant","content":"答"}]`},
	} {
		require.JSONEq(t, c.want, encode(ds.openAIMessages(c.msg)), name)
	}
}

func TestToolInput(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1}`: `{"a":1}`, ``: `{}`, `[1,2]`: `[1,2]`, `{bad`: `"{bad"`, `not json`: `"not json"`,
	} {
		require.Equal(t, want, string(toolInput(in)), in)
	}
}

func TestToolArguments(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1}`: `{"a":1}`, ``: `{}`, `  `: `{}`, `"{bad"`: `{bad`, `[1]`: `[1]`,
	} {
		require.Equal(t, want, toolArguments(json.RawMessage(in)), in)
	}
	// 和 toolInput 互为还原：模型给的参数原样发回。
	for _, args := range []string{`{"a":1}`, `{bad`, `x`} {
		require.Equal(t, args, toolArguments(toolInput(args)), args)
	}
}

func TestRandomCallID(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := randomCallID()
		require.Regexp(t, `^call_[0-9a-f]{16}$`, id)
		require.False(t, seen[id], "不重复")
		seen[id] = true
	}
}

func TestOpenAIStopReason(t *testing.T) {
	for raw, want := range map[string]string{
		"stop": StopEnd, "length": StopMaxTokens, "tool_calls": StopToolUse, "function_call": StopToolUse,
		"content_filter": StopRefusal, "": StopOther, "insufficient_system_resource": StopOther,
	} {
		require.Equal(t, want, openAIStopReason(raw), raw)
	}
}

func TestParseOpenAIError(t *testing.T) {
	for payload, want := range map[string][2]string{
		`{"error":{"message":"bad key","type":"invalid_request_error","code":"invalid_api_key"}}`: {"invalid_request_error", "bad key"},
		`{"error":{"message":"bad key","type":null,"code":"invalid_api_key"}}`:                    {"invalid_api_key", "bad key"},
		`{"error":{"message":"slow","code":429}}`:                                                 {"429", "slow"},
		`{"error":{"message":"m","code":null}}`:                                                   {"", "m"},
		`{"error":{"message":"m"}}`:                                                               {"", "m"},
		`<html>502</html>`:                                                                        {"", ""},
	} {
		kind, message := parseOpenAIError([]byte(payload))
		require.Equal(t, want, [2]string{kind, message}, payload)
	}
}

func TestOpenAIChatErrors(t *testing.T) {
	rec := &recorder{t: t, replies: []string{`{"choices":[]}`, `{"choices":[{"finish_reason":"stop","message":{"content":123}}]}`}}
	server := rec.serve()
	client, err := New(custom(config.AIAPIOpenAI, server.URL), "")
	require.NoError(t, err)
	req := Request{Messages: []Message{{Role: RoleUser, Text: "q"}}}
	_, err = client.Chat(context.Background(), req)
	require.ErrorContains(t, err, "choices 为空")
	_, err = client.Chat(context.Background(), req)
	require.ErrorContains(t, err, "既不是字符串也不是内容块数组")
	_, err = client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleAssistant}}})
	require.ErrorContains(t, err, "第 1 条消息（assistant）是空的", "空消息不发请求")
	require.Len(t, rec.bodies, 2)
}
