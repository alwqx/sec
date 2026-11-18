// aieval 对真实模型手动运行离线评测集：逐条用 sec ask 的 llm.Asker 回答 testdata/aieval 里的问题，检查调用的工具、
// 必须出现和禁止出现的词、截止日期有没有写出，统计数字核对失败率和违规次数。需要联网和模型配置，不进 CI；设计见 docs/ai.md 的「测试」。
//
//	go run ./scripts/aieval                         # 默认模型配置，全部问题
//	go run ./scripts/aieval -ai ds -run valuation   # 指定配置，只跑 id 匹配的问题
//	go run ./scripts/aieval -json > result.json     # 输出每条的回答和检查结果
//
// 每条问题完成后在 stderr 显示一行结果，最后在 stdout 输出汇总；有问题没通过或出错时退出 1。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/cmd/macro"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
)

// caseTimeout 是一条问题的时限，和 sec ask 一致。
const caseTimeout = 10 * time.Minute

// Case 是评测集里的一条问题。tools 和 must 的每一项可以用「|」写几个候选，出现其中一个即可。
type Case struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Code     string   `json:"code,omitempty"`
	Tools    []string `json:"tools,omitempty"`  // 必须调用的工具（失败的调用也算调用过）
	Must     []string `json:"must,omitempty"`   // 回答里必须出现的词，如口径的说法
	Forbid   []string `json:"forbid,omitempty"` // 回答里不能出现的词
}

// Result 是一条问题的检查结果。
type Result struct {
	ID           string      `json:"id"`
	Question     string      `json:"question"`
	Tools        []string    `json:"tools"`         // 实际调用的工具，按顺序
	MissingTools []string    `json:"missing_tools"` // 没有调用的期望工具
	MissingMust  []string    `json:"missing_must"`  // 回答里没有的必须词
	Forbidden    []string    `json:"forbidden"`     // 回答里出现的禁止词
	AdviceTerms  []string    `json:"advice_terms"`  // 数字核对发现的投资建议用语
	Numbers      int         `json:"numbers"`       // 参与核对的数字个数
	Unsourced    []string    `json:"unsourced"`     // 找不到来源的数字
	UndatedAsOf  []string    `json:"undated_as_of"` // 成功的工具调用里、回答没写出的截止日期
	FailedTools  int         `json:"failed_tools"`  // 失败的工具调用次数
	Truncated    bool        `json:"truncated"`     // 回答达到输出上限被截断，后面的内容没有检查
	ElapsedMS    int64       `json:"elapsed_ms"`
	Error        string      `json:"error,omitempty"`
	Answer       *llm.Answer `json:"answer,omitempty"`
}

// Violations 是违规次数：缺少工具、缺少必须词、出现禁止词和投资建议用语、没写出截止日期（提示词要求写明每项数据的
// 截止日期，不管怎么措辞）、回答被截断。数字核对单独统计，不算违规：核对本身有误报，误报率要从评测结果里看。
func (r Result) Violations() int {
	n := len(r.MissingTools) + len(r.MissingMust) + len(r.Forbidden) + len(r.AdviceTerms) + len(r.UndatedAsOf)
	if r.Truncated {
		n++
	}
	return n
}

// Passed 表示没有出错，也没有违规。
func (r Result) Passed() bool { return r.Error == "" && r.Violations() == 0 }

// Summary 是一次评测的汇总。
type Summary struct {
	Model         string `json:"model"`
	Profile       string `json:"profile"`
	PromptVersion string `json:"prompt_version"`
	Cases         int    `json:"cases"`
	Passed        int    `json:"passed"`
	Errors        int    `json:"errors"`
	// 数字核对：有找不到来源的数字的回答数，以及找不到来源的数字占全部数字的比例。
	UnsourcedAnswers int `json:"unsourced_answers"`
	UnsourcedNumbers int `json:"unsourced_numbers"`
	Numbers          int `json:"numbers"`
	// 违规次数，按类型。
	MissingTools int `json:"missing_tools"`
	MissingMust  int `json:"missing_must"`
	Forbidden    int `json:"forbidden"`
	AdviceTerms  int `json:"advice_terms"`
	UndatedAsOf  int `json:"undated_as_of"`
	Truncated    int `json:"truncated"`
	FailedTools  int `json:"failed_tools"`
}

var caseID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// loadCases 读取并校验评测集：id 唯一，问题和证券能被 sec ask 接受，工具在 tools 里，候选词不为空。
func loadCases(path string, tools *aitool.Registry) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		return nil, fmt.Errorf("%s：%w", path, err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("%s 没有问题", path)
	}
	seen := map[string]bool{}
	for i, c := range cases {
		where := fmt.Sprintf("%s 第 %d 条（%s）", path, i+1, c.ID)
		if !caseID.MatchString(c.ID) || seen[c.ID] {
			return nil, fmt.Errorf("%s：id 要唯一，只用小写字母、数字和「-」", where)
		}
		seen[c.ID] = true
		if _, err := llm.NormalizeAskQuestion(c.Question); err != nil {
			return nil, fmt.Errorf("%s：%w", where, err)
		}
		if _, err := llm.NormalizeAskCode(c.Code); err != nil {
			return nil, fmt.Errorf("%s：%w", where, err)
		}
		for _, item := range slices.Concat(c.Tools, c.Must, c.Forbid) {
			if slices.Contains(strings.Split(item, "|"), "") {
				return nil, fmt.Errorf("%s：%q 里有空的候选", where, item)
			}
		}
		for _, item := range c.Tools {
			for _, name := range strings.Split(item, "|") {
				if _, ok := tools.Lookup(name); !ok {
					return nil, fmt.Errorf("%s：没有工具 %s", where, name)
				}
			}
		}
	}
	return cases, nil
}

// score 按评测集的要求检查一个回答。
func score(c Case, answer *llm.Answer) Result {
	r := Result{ID: c.ID, Question: c.Question, Tools: []string{}, Truncated: answer.Truncated, Answer: answer}
	for _, step := range answer.Steps {
		r.Tools = append(r.Tools, step.Tool)
		if step.Error != "" {
			r.FailedTools++
		}
	}
	for _, item := range c.Tools {
		if !slices.ContainsFunc(strings.Split(item, "|"), func(name string) bool { return slices.Contains(r.Tools, name) }) {
			r.MissingTools = append(r.MissingTools, item)
		}
	}
	for _, item := range c.Must {
		if !slices.ContainsFunc(strings.Split(item, "|"), func(word string) bool { return strings.Contains(answer.Text, word) }) {
			r.MissingMust = append(r.MissingMust, item)
		}
	}
	for _, word := range c.Forbid {
		if strings.Contains(answer.Text, word) {
			r.Forbidden = append(r.Forbidden, word)
		}
	}
	for _, term := range answer.Check.AdviceTerms {
		r.AdviceTerms = append(r.AdviceTerms, term.Text)
	}
	r.Numbers = answer.Check.Numbers
	for _, n := range answer.Check.Unsourced {
		r.Unsourced = append(r.Unsourced, n.Text)
	}
	for _, step := range answer.Steps {
		if step.Error == "" && step.AsOf != "" && !mentionsDate(answer.Text, step.AsOf) && !slices.Contains(r.UndatedAsOf, step.AsOf) {
			r.UndatedAsOf = append(r.UndatedAsOf, step.AsOf)
		}
	}
	return r
}

var quarterPattern = regexp.MustCompile(`^(\d{4})Q([1-4])$`)

// mentionsDate 判断回答有没有写出截止日期，接受常见的几种写法：2026-11-17、2026年11月17日、2026/11/17、
// 2026.11.17，月度数据 2026-10、2026年10月，季度数据 2026Q3、2026年第三季度、2026年三季度、2026年第3季度。
// 其他格式原样查找。比较前去掉回答里的空格，「2026 年 11 月 17 日」也算。
func mentionsDate(text, asOf string) bool {
	text = strings.ReplaceAll(text, " ", "")
	var forms []string
	if t, err := time.Parse(time.DateOnly, asOf); err == nil {
		forms = []string{asOf, t.Format("2006/01/02"), t.Format("2006.01.02"), t.Format("2006年1月2日"), t.Format("2006年01月02日")}
	} else if t, err := time.Parse("2006-01", asOf); err == nil {
		forms = []string{asOf, t.Format("2006年1月"), t.Format("2006年01月")}
	} else if m := quarterPattern.FindStringSubmatch(asOf); m != nil {
		year, q := m[1], m[2]
		cn := map[string]string{"1": "一", "2": "二", "3": "三", "4": "四"}[q]
		forms = []string{asOf, year + "年第" + cn + "季度", year + "年" + cn + "季度", year + "年第" + q + "季度", year + "年Q" + q}
	} else {
		forms = []string{asOf}
	}
	return slices.ContainsFunc(forms, func(form string) bool { return strings.Contains(text, form) })
}

// summarize 汇总各条结果。模型名优先用回答里厂商实际返回的；全部出错时用配置里的，仍然知道是哪个配置。
func summarize(results []Result, model llm.Model) Summary {
	s := Summary{Model: model.Name, Profile: model.Profile, Cases: len(results), PromptVersion: llm.AskPromptVersion}
	served := false
	for _, r := range results {
		if r.Answer != nil && r.Answer.Model != "" && !served {
			s.Model, served = r.Answer.Model, true
		}
		if r.Passed() {
			s.Passed++
		}
		if r.Error != "" {
			s.Errors++
		}
		if len(r.Unsourced) > 0 {
			s.UnsourcedAnswers++
		}
		s.UnsourcedNumbers += len(r.Unsourced)
		s.Numbers += r.Numbers
		s.MissingTools += len(r.MissingTools)
		s.MissingMust += len(r.MissingMust)
		s.Forbidden += len(r.Forbidden)
		s.AdviceTerms += len(r.AdviceTerms)
		s.UndatedAsOf += len(r.UndatedAsOf)
		if r.Truncated {
			s.Truncated++
		}
		s.FailedTools += r.FailedTools
	}
	return s
}

// run 逐条回答问题，每条完成后把一行结果写到 progress。ctx 取消时停止，返回已经完成的结果。
func run(ctx context.Context, asker *llm.Asker, cases []Case, progress io.Writer) []Result {
	var results []Result
	for i, c := range cases {
		if ctx.Err() != nil {
			break
		}
		caseCtx, cancel := context.WithTimeout(ctx, caseTimeout)
		start := time.Now()
		answer, err := asker.Ask(caseCtx, c.Question, c.Code)
		cancel()
		var r Result
		if err != nil {
			r = Result{ID: c.ID, Question: c.Question, Error: caseError(err)}
			if ctx.Err() != nil {
				break
			}
		} else {
			r = score(c, answer)
		}
		r.ElapsedMS = time.Since(start).Milliseconds()
		results = append(results, r)
		fmt.Fprintf(progress, "[%d/%d] %s\n", i+1, len(cases), resultLine(r))
	}
	return results
}

// caseError 是一条问题出错的说明。模型接口的错误只给 llm.PublicError 的部分：厂商的错误正文可能回显 Key，
// 而结果会显示在终端、写进 -json 的文件。
func caseError(err error) string {
	if public, ok := llm.PublicError(err); ok {
		return public
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("超过 %d 分钟没有完成", int(caseTimeout.Minutes()))
	}
	return err.Error()
}

// resultLine 是一条结果的一行说明。
func resultLine(r Result) string {
	elapsed := strconv.FormatFloat(float64(r.ElapsedMS)/1000, 'f', 1, 64) + "s"
	if r.Error != "" {
		return fmt.Sprintf("%s 出错：%s · %s", r.ID, r.Error, elapsed)
	}
	var problems []string
	add := func(label string, items []string) {
		if len(items) > 0 {
			problems = append(problems, label+strings.Join(items, "、"))
		}
	}
	add("缺少工具 ", r.MissingTools)
	add("缺少 ", r.MissingMust)
	add("出现禁止词 ", r.Forbidden)
	add("投资建议用语 ", r.AdviceTerms)
	add("没写出截止日期 ", r.UndatedAsOf)
	if r.Truncated {
		problems = append(problems, "回答被截断")
	}
	status := "通过"
	if len(problems) > 0 {
		status = "未通过：" + strings.Join(problems, "；")
	}
	tools := strings.Join(r.Tools, ",")
	if tools == "" {
		tools = "没有调用工具"
	}
	return fmt.Sprintf("%s %s · %d 轮 · %s · 数字 %d/%d 找到来源 · %s", r.ID, status, r.Answer.Rounds, tools, r.Numbers-len(r.Unsourced), r.Numbers, elapsed)
}

// printSummary 输出汇总。
func printSummary(out io.Writer, s Summary) {
	fmt.Fprintf(out, "模型 %s · 配置 %s · 提示词 %s\n", s.Model, s.Profile, s.PromptVersion)
	fmt.Fprintf(out, "通过 %d/%d，出错 %d\n", s.Passed, s.Cases, s.Errors)
	answered := s.Cases - s.Errors
	fmt.Fprintf(out, "数字核对：%d/%d 个回答有找不到来源的数字（%s），共 %d/%d 个数字（%s）\n",
		s.UnsourcedAnswers, answered, percent(s.UnsourcedAnswers, answered), s.UnsourcedNumbers, s.Numbers, percent(s.UnsourcedNumbers, s.Numbers))
	fmt.Fprintf(out, "违规：缺少工具 %d 次，缺少必须词 %d 次，禁止词 %d 次，投资建议用语 %d 次，没写出截止日期 %d 个，回答被截断 %d 条\n",
		s.MissingTools, s.MissingMust, s.Forbidden, s.AdviceTerms, s.UndatedAsOf, s.Truncated)
	fmt.Fprintf(out, "工具调用失败 %d 次\n", s.FailedTools)
}

func percent(n, total int) string {
	if total == 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(n)*100/float64(total), 'f', 1, 64) + "%"
}

// openAsker 按配置名创建问答，和 sec ask 用同样的工具。配置文件出错时的处理和 sec ask（aicli.Open）一致：
// ai 段无效时停下，不能当成没有模型配置，照着提示添加配置会覆盖文件、丢掉已存的 Key。
func openAsker(ctx context.Context, profile string, tools *aitool.Registry) (*llm.Asker, error) {
	cfg, err := config.Load(ctx)
	switch {
	case errors.Is(err, config.ErrInvalidAI):
		return nil, fmt.Errorf("%w；请先手动修正 ai 段", err)
	case errors.Is(err, config.ErrInvalid) && len(cfg.AI.Profiles) == 0:
		return nil, fmt.Errorf("%w；读不到模型配置，请先修正配置文件", err)
	case errors.Is(err, config.ErrInvalid):
		fmt.Fprintln(os.Stderr, err)
	case err != nil:
		return nil, err
	}
	secret, err := config.DefaultAISecret()
	if err != nil {
		return nil, err
	}
	client, model, err := llm.Open(&cfg.AI, profile, secret, os.Getenv)
	if err != nil {
		return nil, err
	}
	return &llm.Asker{Client: client, Model: model, Tools: tools}, nil
}

func main() {
	cases := flag.String("cases", "testdata/aieval/ask.json", "评测集文件")
	profile := flag.String("ai", "", "模型配置名，为空时用默认配置")
	filter := flag.String("run", "", "只运行 id 匹配这个正则的问题")
	asJSON := flag.Bool("json", false, "输出 JSON：每条的回答、检查结果和汇总")
	flag.Parse()
	if err := mainErr(*cases, *profile, *filter, *asJSON); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var errFailed = errors.New("有问题没有通过或出错")

func mainErr(path, profile, filter string, asJSON bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	tools := aitool.Default(macro.AISource())
	cases, err := loadCases(path, tools)
	if err != nil {
		return err
	}
	if filter != "" {
		pattern, err := regexp.Compile(filter)
		if err != nil {
			return fmt.Errorf("-run：%w", err)
		}
		cases = slices.DeleteFunc(cases, func(c Case) bool { return !pattern.MatchString(c.ID) })
		if len(cases) == 0 {
			return fmt.Errorf("没有 id 匹配 %s 的问题", filter)
		}
	}
	asker, err := openAsker(ctx, profile, tools)
	if err != nil {
		return err
	}
	results := run(ctx, asker, cases, os.Stderr)
	summary := summarize(results, asker.Model)
	if asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]any{"summary": summary, "results": results}); err != nil {
			return err
		}
	} else {
		printSummary(os.Stdout, summary)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("已取消，完成 %d/%d 条", len(results), len(cases))
	}
	if summary.Passed < summary.Cases {
		return errFailed
	}
	return nil
}
