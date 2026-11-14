package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/valuation"
)

// aiValuationInput 只接收证券、临时配置名和 DCF 参数，不接受前端提交的估值或提示词。
type aiValuationInput struct {
	Code           string   `json:"code"`
	Profile        string   `json:"profile"`
	Growth         *float64 `json:"growth"`
	TerminalGrowth *float64 `json:"terminal_growth"`
	WACC           *float64 `json:"wacc"`
	MarginOfSafety *float64 `json:"margin_of_safety"`
}

func (in aiValuationInput) params() valuation.DCFParams {
	p := valuation.DefaultDCFParams()
	p.Growth = in.Growth
	if in.TerminalGrowth != nil {
		p.TerminalGrowth = *in.TerminalGrowth
	}
	if in.WACC != nil {
		p.WACC = *in.WACC
	}
	if in.MarginOfSafety != nil {
		p.MarginOfSafety = *in.MarginOfSafety
	}
	return p
}

// toolInput 只包含给出的参数，字段名与 CLI 的 explainInput 一致，复用同一个解读缓存。
func (in aiValuationInput) toolInput() (json.RawMessage, error) {
	input := map[string]any{"code": in.Code}
	for key, value := range map[string]*float64{
		"growth": in.Growth, "terminal_growth": in.TerminalGrowth, "wacc": in.WACC, "margin_of_safety": in.MarginOfSafety,
	} {
		if value != nil {
			input[key] = *value
		}
	}
	return json.Marshal(input)
}

// handleAIValuation 在服务器计算估值，复用 llm 的精简输出、提示词、缓存、数字核对和免责声明。
// 每次读取最新配置，临时选择的 profile 不写回 default；AI 没有启用时不取数、不调用模型。
func (s *desktopServer) handleAIValuation(w http.ResponseWriter, r *http.Request) {
	var in aiValuationInput
	if !s.readAIRequest(w, r, &in) {
		return
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	if !aShareCode.MatchString(in.Code) && !usTicker.MatchString(in.Code) {
		writeError(w, http.StatusBadRequest, "估值解读只支持带交易所的 A 股代码或 $ 开头的美股代码")
		return
	}
	params := in.params()
	if err := params.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input, err := in.toolInput()
	if err != nil {
		writeError(w, http.StatusBadRequest, "DCF 参数无效")
		return
	}
	client, model, _, ok := s.openAIModel(w, r, in.Profile)
	if !ok {
		return
	}
	if wantsAIStream(r) {
		s.streamAIValuation(w, r, in.Code, params, input, client, model)
		return
	}
	result := s.queryValuation(w, r, in.Code, params)
	if result == nil {
		return
	}
	explainer := valuationExplainer(r.Context(), in.Code, result, client, model)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	explanation, err := explainer.Explain(ctx, input)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, message := explanationFailure(ctx, err)
		writeError(w, status, message)
		return
	}
	writeJSON(w, explanation)
}

func valuationExplainer(ctx context.Context, code string, result *valuation.Result, client llm.Client, model llm.Model) *llm.ValuationExplainer {
	cacheDir, err := llm.DefaultCacheDir("valuation")
	if err != nil {
		slog.WarnContext(ctx, "无法创建解读缓存目录，本次不缓存", "error", err)
	}
	return &llm.ValuationExplainer{Client: client, Model: model, CacheDir: cacheDir,
		Fetch: func(context.Context, json.RawMessage) (*aitool.Result, error) {
			return aitool.ValuationResult(&sina.BasicSecurity{ExCode: code, Name: result.Name}, result), nil
		},
	}
}

func explanationFailure(ctx context.Context, err error) (int, string) {
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return http.StatusGatewayTimeout, "模型解读超时，请稍后重试或选择其他模型"
	}
	// 厂商错误正文可能回显 Key，不回传也不写日志。
	if message, ok := llm.PublicError(err); ok {
		return http.StatusBadGateway, message + "，请检查模型配置或稍后重试"
	}
	return http.StatusBadGateway, "估值解读失败，流式回复可能中断或模型没有返回可用解读，请检查模型配置或稍后重试"
}
