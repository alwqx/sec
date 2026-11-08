package macro

import (
	"context"
	"errors"

	"github.com/alwqx/sec/aitool"
)

// AISource 把宏观目录和统一导出交给 aitool 的 get_macro_series，sec mcp 和以后的 sec ask、Desktop 对话共用。
func AISource() aitool.MacroSource { return aiSource{} }

type aiSource struct{}

func (aiSource) Indicators() []aitool.MacroIndicator {
	titles := map[string]string{}
	for _, category := range categories {
		titles[category.ID] = category.Title
	}
	out := make([]aitool.MacroIndicator, 0, len(indicators))
	for _, item := range indicators {
		indicator := aitool.MacroIndicator{Region: item.Region, Command: item.Command, Title: item.Title, Category: titles[item.Category], Period: item.Period, DefaultBegin: item.DefaultBegin, EarliestBegin: item.EarliestBegin}
		for _, option := range item.Options {
			values := []string{}
			for _, value := range option.Values {
				values = append(values, value.Value)
			}
			indicator.Options = append(indicator.Options, aitool.MacroOption{Flag: option.Flag, Label: option.Label, Values: values})
		}
		out = append(out, indicator)
	}
	return out
}

// Export 运行指标命令的 --standard-json；指标或选项不在清单内时返回参数错误。
func (aiSource) Export(ctx context.Context, query aitool.MacroQuery) ([]byte, error) {
	raw, err := Export(ctx, ExportQuery{Region: query.Region, Command: query.Command, Begin: query.Begin, End: query.End, Options: query.Options})
	if errors.Is(err, ErrInvalidQuery) {
		return nil, aitool.Inputf("%v", err)
	}
	return raw, err
}
