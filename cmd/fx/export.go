package fx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/alwqx/sec/provider/forex"
)

// ErrInvalidQuery 表示请求的命令、参数取值或日期格式无效。
var ErrInvalidQuery = errors.New("无效的汇率查询")

// ExportQuery 是一次汇率查询。与所选命令和来源无关的字段不会传给命令。
type ExportQuery struct {
	Command, Source string
	Begin, End      string
	Pair            string
	At              string
	MaxSkew, MaxAge string
	Refresh         bool
}

var (
	exportSource = map[string][]string{"history": {"sina", "fred"}, "cross": {"fred", "cfets", "sina"}}
	exportAt     = []string{"10:00", "11:00", "14:00", "15:00", "16:00"}
	exportSkew   = []string{"0s", "60s"}
	exportAge    = []string{"15m", "1h", "24h"}
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuery, fmt.Sprintf(format, args...))
}

func validDate(value string) bool {
	parsed, err := time.Parse("20060102", value)
	return err == nil && parsed.Format("20060102") == value
}

// exportArgs 只组装所选命令和来源支持的参数：例如新浪交叉汇率用最近报价，不接受日期。
func exportArgs(query ExportQuery) ([]string, error) {
	if !slices.Contains([]string{"quote", "history", "fixing", "reference", "cross", "spread"}, query.Command) {
		return nil, invalid("未知命令 %s", query.Command)
	}
	args := []string{query.Command, "--json"}
	source := query.Source
	if sources, ok := exportSource[query.Command]; ok {
		if source == "" {
			source = sources[0]
		}
		if !slices.Contains(sources, source) {
			return nil, invalid("不支持来源 %s", source)
		}
		args = append(args, "--source="+source)
	}
	daily := query.Command != "quote" && query.Command != "spread" && !(query.Command == "cross" && source == "sina")
	if daily {
		for _, value := range []string{query.Begin, query.End} {
			if value != "" && !validDate(value) {
				return nil, invalid("日期 %q 格式无效", value)
			}
		}
		if query.Begin != "" && query.End != "" && query.Begin > query.End {
			return nil, invalid("开始日期不能晚于截至日期")
		}
		if query.Begin != "" {
			args = append(args, "--begin="+query.Begin)
		}
		if query.End != "" {
			args = append(args, "--end="+query.End)
		}
	}
	if query.Command == "cross" && query.Pair != "" {
		if !slices.Contains(forex.CrossPairs(source), query.Pair) {
			return nil, invalid("来源 %s 不支持货币对 %q", source, query.Pair)
		}
		args = append(args, "--pairs="+query.Pair)
	}
	if query.At != "" && (query.Command == "reference" || (query.Command == "cross" && source == "cfets")) {
		if !slices.Contains(exportAt, query.At) {
			return nil, invalid("不支持时点 %s", query.At)
		}
		args = append(args, "--at="+query.At)
	}
	if query.Command == "spread" || (query.Command == "cross" && source == "sina") {
		if query.MaxSkew != "" {
			if !slices.Contains(exportSkew, query.MaxSkew) {
				return nil, invalid("不支持时间差 %s", query.MaxSkew)
			}
			args = append(args, "--max-skew="+query.MaxSkew)
		}
		if query.MaxAge != "" {
			if !slices.Contains(exportAge, query.MaxAge) {
				return nil, invalid("不支持报价年龄 %s", query.MaxAge)
			}
			args = append(args, "--max-age="+query.MaxAge)
		}
	}
	if query.Refresh {
		args = append(args, "--refresh")
	}
	return args, nil
}

// Export 在进程内运行汇率命令的 --json，返回保留来源和口径的 JSON。
func Export(ctx context.Context, query ExportQuery) ([]byte, error) {
	args, err := exportArgs(query)
	if err != nil {
		return nil, err
	}
	root := NewFXCLI()
	var out bytes.Buffer
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SilenceUsage, root.SilenceErrors = true, true
	if err := root.ExecuteContext(ctx); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
