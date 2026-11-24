package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/alwqx/sec/utils"
	"github.com/stretchr/testify/require"
)

func TestDecode(t *testing.T) {
	var in quoteInput
	require.NoError(t, Decode(json.RawMessage(`{"codes":["600036"]}`), &in))
	require.Equal(t, []string{"600036"}, in.Codes)

	// 空参数按 {} 处理。
	in = quoteInput{}
	require.NoError(t, Decode(nil, &in))
	require.Nil(t, in.Codes)

	for _, input := range []string{`{"code":"600036"}`, `{"codes":"600036"}`, `{"codes":[]} {}`, `[`} {
		err := Decode(json.RawMessage(input), &in)
		require.Error(t, err, input)
		require.True(t, IsInputError(err), input)
	}
}

func TestNewRegistry(t *testing.T) {
	call := func(context.Context, json.RawMessage) (*Result, error) { return nil, nil }
	tool := func(name string) Tool {
		return Tool{Name: name, Description: "说明", InputSchema: json.RawMessage(`{"type":"object"}`), Call: call}
	}

	r, err := NewRegistry(tool("b_tool"), tool("a_tool"))
	require.NoError(t, err)
	names := []string{}
	for _, tool := range r.Tools() {
		names = append(names, tool.Name)
	}
	require.Equal(t, []string{"b_tool", "a_tool"}, names, "按注册顺序")
	found, ok := r.Lookup("a_tool")
	require.True(t, ok)
	require.Equal(t, "a_tool", found.Name)
	_, ok = r.Lookup("missing")
	require.False(t, ok)

	_, err = NewRegistry(tool("a_tool"), tool("a_tool"))
	require.ErrorContains(t, err, "重复")
	_, err = NewRegistry(tool("GetQuote"))
	require.ErrorContains(t, err, "不合法")
	noCall := tool("a_tool")
	noCall.Call = nil
	_, err = NewRegistry(noCall)
	require.ErrorContains(t, err, "缺少")
	arraySchema := tool("a_tool")
	arraySchema.InputSchema = json.RawMessage(`{"type":"array"}`)
	_, err = NewRegistry(arraySchema)
	require.ErrorContains(t, err, "object")
}

func TestDefault(t *testing.T) {
	r := Default(nil)
	for _, name := range []string{"search_security", "get_quote"} {
		_, ok := r.Lookup(name)
		require.True(t, ok, name)
	}
}

func TestResultJSON(t *testing.T) {
	data, err := json.Marshal(NewResult("get_quote", "新浪财经行情", map[string]int{"a": 1}))
	require.NoError(t, err)
	require.JSONEq(t, `{"schema_version":"1","tool":"get_quote","source":"新浪财经行情","notes":[],"data":{"a":1}}`, string(data))
}

// TestRegistryRecoversPanic 是注册表里的工具 panic 时只算这次调用失败（ErrPanic），之后的调用照常执行。
func TestRegistryRecoversPanic(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	r, err := NewRegistry(Tool{Name: "boom", Description: "说明", InputSchema: schema,
		Call: func(_ context.Context, input json.RawMessage) (*Result, error) {
			if string(input) == `{"ok":true}` {
				return NewResult("boom", "测试", nil), nil
			}
			var m map[string]int
			m["x"] = 1 // 运行时错误的 panic
			return nil, nil
		}})
	require.NoError(t, err)
	tool, ok := r.Lookup("boom")
	require.True(t, ok)
	result, err := tool.Call(context.Background(), json.RawMessage(`{}`))
	require.Nil(t, result)
	require.True(t, errors.Is(err, ErrPanic))
	require.False(t, IsInputError(err))
	require.Equal(t, "boom 执行出错", err.Error(), "不把 panic 的值交给模型")
	require.True(t, IsPanic(err))
	require.False(t, ShouldLog(err), "panic 已经记过日志")
	subPanic := fmt.Errorf("估值计算失败：%w", utils.ErrPanic)
	require.True(t, IsPanic(subPanic))
	require.False(t, ShouldLog(subPanic), "子 goroutine panic 已经记过日志")
	require.False(t, IsPanic(errors.New("上游失败")))
	require.False(t, ShouldLog(Inputf("参数错误")))
	require.False(t, ShouldLog(nil))
	require.True(t, ShouldLog(errors.New("上游失败")))

	result, err = r.Tools()[0].Call(context.Background(), json.RawMessage(`{"ok":true}`))
	require.NoError(t, err)
	require.Equal(t, "boom", result.Tool)
}
