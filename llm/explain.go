package llm

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/watchlist"
)

// 提示词放在 llm/prompts/，随代码审查。改提示词时同时改 ValuationPromptVersion；缓存键还包含提示词内容的摘要，
// 忘了改版本号也不会用到旧提示词的缓存。
var (
	//go:embed prompts/system.md
	systemPrompt string
	//go:embed prompts/valuation_explain.md
	valuationPrompt string
)

// ValuationPromptVersion 是估值解读提示词的版本，写进结果和缓存键。
const ValuationPromptVersion = "valuation-explain-v1"

// explainMaxTokens 是解读的输出上限：300 字的中文约 600 个 token，要给思考模型留出推理的余量。
const explainMaxTokens = 4000

// defaultDisclaimer 是工具结果没有 disclaimer 时附上的说明。
const defaultDisclaimer = "以上由模型根据 sec 的数据生成，只解释数据，不构成投资建议。"

// Explanation 是一次估值解读的结果，CLI 的 --json 和 Desktop 接口原样输出。
type Explanation struct {
	ExCode        string    `json:"ex_code"`
	Name          string    `json:"name"`
	AsOf          string    `json:"as_of"`      // 估值数据的截止日期
	Text          string    `json:"text"`       // 模型的解读，不含免责声明
	Disclaimer    string    `json:"disclaimer"` // 来自工具结果，由代码附上，不靠模型
	Check         Check     `json:"check"`      // 数字核对结果
	Truncated     bool      `json:"truncated"`  // 输出达到上限被截断
	Model         string    `json:"model"`      // 厂商返回的实际模型
	Profile       string    `json:"profile"`    // 使用的模型配置
	PromptVersion string    `json:"prompt_version"`
	Cached        bool      `json:"cached"` // 这次是不是从缓存读的
	CreatedAt     time.Time `json:"created_at"`
}

// ExplanationMeta 在正文之前发送，提前说明数据日期、模型和免责声明。
type ExplanationMeta struct {
	ExCode     string `json:"ex_code"`
	Name       string `json:"name"`
	AsOf       string `json:"as_of"`
	Model      string `json:"model"`
	Profile    string `json:"profile"`
	Disclaimer string `json:"disclaimer"`
	Cached     bool   `json:"cached"`
}

type ExplanationEvent struct {
	Type string
	Text string
	Meta *ExplanationMeta
}

// ValuationExplainer 生成估值解读：调用 get_valuation 取精简输出，单轮请求模型（不开放工具调用），
// 附上工具结果里的 disclaimer，并做数字核对。按 证券 + as_of + 模型 + 提示词 + 参数 缓存，同一份数据不重复调用模型。
type ValuationExplainer struct {
	Client Client
	Model  Model
	// CacheDir 为空时不缓存。CLI 和 Desktop 用同一个目录，见 DefaultCacheDir。
	CacheDir string
	// Fetch 取 get_valuation 的结果，为空时调用 aitool.ValuationTool，测试时替换。
	Fetch func(ctx context.Context, input json.RawMessage) (*aitool.Result, error)
}

// DefaultCacheDir 是解读缓存的默认目录 ~/.sec/cache/ai/<kind>，CLI 和 Desktop 共用。
func DefaultCacheDir(kind string) (string, error) {
	return utils.SecDir("cache", "ai", kind)
}

// Explain 解读一只股票的估值。input 是 get_valuation 的参数，如 {"code":"600519"}，也可以带 DCF 参数。
func (e *ValuationExplainer) Explain(ctx context.Context, input json.RawMessage) (*Explanation, error) {
	return e.explain(ctx, input, nil, false)
}

// ExplainStream 发送尚未核对的正文增量，正常结束后才核对和缓存；返回最终完整结果。
func (e *ValuationExplainer) ExplainStream(ctx context.Context, input json.RawMessage, emit func(ExplanationEvent) error) (*Explanation, error) {
	return e.explain(ctx, input, emit, true)
}

func (e *ValuationExplainer) explain(ctx context.Context, input json.RawMessage, emit func(ExplanationEvent) error, streaming bool) (*Explanation, error) {
	output := func(event ExplanationEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if emit != nil {
			return emit(event)
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fetch := e.Fetch
	if fetch == nil {
		fetch = aitool.ValuationTool().Call
	}
	result, err := fetch(ctx, input)
	if err != nil {
		return nil, err
	}
	source, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var data struct {
		ExCode string `json:"ex_code"`
		Name   string `json:"name"`
	}
	if raw, err := json.Marshal(result.Data); err == nil {
		_ = json.Unmarshal(raw, &data)
	}

	key, err := e.cacheKey(data.ExCode, result.AsOf, input)
	if err != nil {
		return nil, err
	}
	disclaimer := result.Disclaimer
	if disclaimer == "" {
		disclaimer = defaultDisclaimer
	}
	meta := &ExplanationMeta{ExCode: data.ExCode, Name: data.Name, AsOf: result.AsOf, Model: e.Model.Name, Profile: e.Model.Profile, Disclaimer: disclaimer}
	if cached := e.readCache(key); cached != nil {
		cached.Profile = e.Model.Profile
		meta.Model, meta.Disclaimer, meta.Cached = cached.Model, cached.Disclaimer, true
		if err := output(ExplanationEvent{Type: "meta", Meta: meta}); err != nil {
			return nil, err
		}
		return cached, nil
	}
	if err := output(ExplanationEvent{Type: "meta", Meta: meta}); err != nil {
		return nil, err
	}

	req := Request{
		System:    systemPrompt,
		Messages:  []Message{{Role: RoleUser, Text: valuationPrompt + "\n\n" + string(source)}},
		MaxTokens: explainMaxTokens,
	}
	var resp *Response
	if streaming {
		client, ok := e.Client.(StreamingClient)
		if !ok {
			return nil, ErrStreamUnsupported
		}
		resp, err = client.ChatStream(ctx, req, func(event StreamEvent) error {
			if event.Type == StreamText {
				return output(ExplanationEvent{Type: "delta", Text: event.Text})
			}
			if event.Type == StreamThinking {
				return output(ExplanationEvent{Type: "status", Text: "模型正在思考…"})
			}
			return nil
		})
	} else {
		resp, err = e.Client.Chat(ctx, req)
	}
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(resp.Message.Text)
	switch {
	case resp.StopReason == StopRefusal:
		return nil, errors.New("模型拒绝了这个请求，换一个模型配置再试")
	case text == "":
		return nil, fmt.Errorf("模型没有给出解读（停止原因 %s）", resp.RawStopReason)
	case resp.StopReason != StopEnd && resp.StopReason != StopMaxTokens:
		return nil, fmt.Errorf("模型没有正常完成解读（停止原因 %s）", resp.RawStopReason)
	}
	if err := output(ExplanationEvent{Type: "status", Text: "正在核对回答…"}); err != nil {
		return nil, err
	}
	explanation := &Explanation{
		ExCode: data.ExCode, Name: data.Name, AsOf: result.AsOf, Text: text, Disclaimer: disclaimer,
		Check: Verify(text, string(source)), Truncated: resp.StopReason == StopMaxTokens,
		Model: resp.Model, Profile: e.Model.Profile, PromptVersion: ValuationPromptVersion, CreatedAt: time.Now(),
	}
	if explanation.Model == "" {
		explanation.Model = e.Model.Name
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 被截断的解读不缓存，下次重新生成。
	if !explanation.Truncated {
		e.writeCache(key, explanation)
	}
	return explanation, nil
}

// cacheKey 由模型、提示词（版本和内容）、证券、数据截止日期和其他参数算出。证券用解析后的 ex_code，
// 600519 和 SH600519 共用缓存；没有 ex_code 或 as_of 时不缓存。
func (e *ValuationExplainer) cacheKey(exCode, asOf string, input json.RawMessage) (string, error) {
	if e.CacheDir == "" || exCode == "" || asOf == "" {
		return "", nil
	}
	var params map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &params); err != nil {
			return "", err
		}
	}
	delete(params, "code")
	// map 编码时按键排序，参数顺序不同也是同一个键。
	canonical, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{e.Model.ID(), ValuationPromptVersion, systemPrompt, valuationPrompt, exCode, asOf, string(canonical)}, "\x00")))
	return hex.EncodeToString(sum[:16]), nil
}

func (e *ValuationExplainer) readCache(key string) *Explanation {
	if key == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(e.CacheDir, key+".json"))
	if err != nil {
		return nil
	}
	var cached Explanation
	if json.Unmarshal(data, &cached) != nil || cached.Text == "" {
		return nil
	}
	cached.Cached = true
	return &cached
}

// writeCache 写缓存，失败不影响这次的结果。
func (e *ValuationExplainer) writeCache(key string, explanation *Explanation) {
	if key == "" {
		return
	}
	data, err := json.MarshalIndent(explanation, "", "  ")
	if err != nil {
		return
	}
	_ = watchlist.AtomicWrite(filepath.Join(e.CacheDir, key+".json"), data)
}
