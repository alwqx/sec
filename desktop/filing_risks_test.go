package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const previousFilingFolder = "/Archives/edgar/data/320193/000032019325000079/"

// riskTenKHTML 生成一份 10-K 主文件，Item 1A 里每个标题是一条粗斜体的风险因素小标题。
func riskTenKHTML(titles ...string) string {
	var b strings.Builder
	b.WriteString(`<p>ITEM 1A. RISK FACTORS</p><p><b>Business Risks</b></p>`)
	for _, title := range titles {
		b.WriteString(`<p><b><i>` + title + `</i></b></p><p>` + strings.Repeat(title+" Costs could rise. ", 40) + `</p>`)
	}
	b.WriteString(`<p>Item 7. Management’s Discussion and Analysis</p><p>` + strings.Repeat("Net sales increased 6%. ", 80) + `</p>`)
	return b.String()
}

// mockRiskFilings 返回今年和上一年的 10-K：今年新增 AI 风险，上一年有气候风险。
func mockRiskFilings() {
	mockFilingIndex("10-K")
	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "aapl-20261102.htm").Reply(200).
		BodyString(riskTenKHTML("The Company depends on component suppliers.", "Artificial intelligence features may not succeed."))
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"cik":"320193","filings":{"recent":{
"accessionNumber":["0000320193-26-000024","0000320193-25-000079"],"filingDate":["2026-11-02","2025-10-31"],"reportDate":["2026-11-02","2025-09-27"],
"form":["10-K","10-K"],"primaryDocument":["aapl-20261102.htm","aapl-20250927.htm"]},"files":[]}}`)
	gock.New("https://www.sec.gov").Get(previousFilingFolder + "0000320193-25-000079-index.htm").Reply(200).BodyString(`<div id="formName"><strong>Form 10-K</strong></div>
<div class="infoHead">Filing Date</div><div class="info">2025-10-31</div><div class="infoHead">Period of Report</div><div class="info">2025-09-27</div>
<table class="tableFile" summary="Document Format Files"><tr><td>1</td><td></td><td><a href="` + previousFilingFolder + `aapl-20250927.htm">x</a></td><td>10-K</td><td>1</td></tr></table>`)
	gock.New("https://www.sec.gov").Get(previousFilingFolder + "aapl-20250927.htm").Reply(200).
		BodyString(riskTenKHTML("The Company depends on component suppliers.", "Climate change could disrupt operations."))
}

// resetRiskCache 清空风险因素对比的内存缓存，测试之间不互相影响。
func resetRiskCache(t *testing.T) {
	t.Helper()
	clear := func() {
		riskChangesCache.Lock()
		riskChangesCache.entries, riskChangesCache.order = map[string]*edgar.RiskChanges{}, nil
		riskChangesCache.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func filingRisksRequest(s *desktopServer, link string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/filings/risks?"+url.Values{"url": {link}}.Encode(), nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func aiFilingRisksRequest(s *desktopServer, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/filings/risks", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestFilingRisks(t *testing.T) {
	resetRiskCache(t)
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	mockRiskFilings()
	w := filingRisksRequest(s, desktopFilingURL)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, gock.IsDone())
	var changes edgar.RiskChanges
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &changes))
	require.Equal(t, "0000320193-25-000079", changes.Previous.Accession)
	require.Equal(t, "Artificial intelligence features may not succeed.", changes.Added[0].Title)
	require.Equal(t, "Climate change could disrupt operations.", changes.Removed[0].Title)
	require.Len(t, changes.Kept, 1)
	require.NotContains(t, w.Body.String(), "Costs could rise", "不返回风险因素正文")

	// 再对比一次、再请求解读都用内存里的对比结果，不再请求 EDGAR。
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	w = filingRisksRequest(s, desktopFilingURL)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	configureAIValuation(t, s)
	gock.New("https://model.example").Post("/v1/chat/completions").Reply(200).JSON(map[string]any{"model": "served", "choices": []any{map[string]any{"finish_reason": "stop",
		"message": map[string]any{"role": "assistant", "content": `{"sections":[{"text":"今年新增人工智能风险。","refs":["新增 1"]}]}`}}}})
	w = aiFilingRisksRequest(s, `{"url":"`+desktopFilingURL+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Empty(t, gock.GetUnmatchedRequests(), "对比结果来自缓存")
	require.True(t, gock.IsDone())
}

func TestFilingRisksErrors(t *testing.T) {
	resetRiskCache(t)
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	require.Equal(t, http.StatusBadRequest, filingRisksRequest(s, "https://evil.test/a.htm").Code)

	mockFilingIndex("8-K")
	w := filingRisksRequest(s, desktopFilingURL)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	require.Contains(t, w.Body.String(), "这份申报是 8-K，不是 10-K 或 10-KT")

	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "0000320193-26-000024-index.htm").Reply(500)
	w = filingRisksRequest(s, desktopFilingURL)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "从 SEC 读取申报失败")
	require.NotContains(t, w.Body.String(), "sec.gov", "下载错误不带上游细节")
}

func TestAIFilingRisks(t *testing.T) {
	resetRiskCache(t)
	defer gock.Off()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)
	require.Equal(t, http.StatusBadRequest, aiFilingRisksRequest(s, `{"url":"https://evil.test/a.htm"}`).Code)
	require.Equal(t, http.StatusConflict, aiFilingRisksRequest(s, `{"url":"`+desktopFilingURL+`","profile":"missing"}`).Code)

	mockRiskFilings()
	gock.New("https://model.example").Post("/v1/chat/completions").MatchHeader("Authorization", "Bearer "+aiValuationKey).
		Reply(200).JSON(map[string]any{"model": "served", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant",
		"content": `{"sections":[{"text":"今年新增人工智能风险，气候风险不再单独列出。","refs":["新增 1","删除 1"]}]}`}}}})
	w := aiFilingRisksRequest(s, `{"url":"`+desktopFilingURL+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result llm.RiskExplanation
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "first", result.Profile)
	require.Equal(t, "served", result.Model)
	require.Equal(t, []string{"新增 1", "删除 1"}, result.Sections[0].Refs)
	require.Equal(t, "0000320193-25-000079", result.Changes.Previous.Accession)
	require.NotContains(t, w.Body.String(), aiValuationKey)

	// 与 CLI 共用缓存，命中时不请求 EDGAR 和模型。
	w = aiFilingRisksRequest(s, `{"url":"`+desktopFilingURL+`","profile":"first"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Cached)
	require.True(t, gock.IsDone())

	// 读申报列表失败按 SEC 下载失败处理，不让用户去换模型。
	resetRiskCache(t)
	mockFilingIndex("10-K")
	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "aapl-20261102.htm").Reply(200).BodyString(riskTenKHTML("The Company depends on component suppliers."))
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(403)
	w = aiFilingRisksRequest(s, `{"url":"`+desktopFilingURL+`","profile":"second"}`)
	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "从 SEC 读取申报失败")

	// 申报内容的问题返回 422 和 sec 生成的原因。
	mockFilingIndex("8-K")
	w = aiFilingRisksRequest(s, `{"url":"https://www.sec.gov/Archives/edgar/data/320193/000032019326000024/other.htm","profile":"second"}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "这份申报是 8-K")
}
