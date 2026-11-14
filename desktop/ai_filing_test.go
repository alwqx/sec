package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const (
	desktopFilingFolder = "/Archives/edgar/data/320193/000032019326000024/"
	desktopFilingURL    = "https://www.sec.gov" + desktopFilingFolder + "aapl-20261102.htm"
)

func aiFilingRequest(s *desktopServer, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/filings/summary", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// mockFilingIndex 返回 SEC 申报目录页，主文件类型为 form。
func mockFilingIndex(form string) {
	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "0000320193-26-000024-index.htm").Reply(200).BodyString(`<div id="formName"><strong>Form ` + form + `</strong></div>
<div class="infoHead">Filing Date</div><div class="info">2026-11-02</div><div class="infoHead">Period of Report</div><div class="info">2026-11-02</div>
<table class="tableFile" summary="Document Format Files"><tr><th>Seq</th></tr>
<tr><td>1</td><td></td><td><a href="/ix?doc=` + desktopFilingFolder + `aapl-20261102.htm">x</a></td><td>` + form + `</td><td>1</td></tr>
<tr><td>2</td><td></td><td><a href="` + desktopFilingFolder + `ex991.htm">x</a></td><td>EX-99.1</td><td>1</td></tr></table>`)
}

func mockFilingDocuments() {
	mockFilingIndex("8-K")
	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "aapl-20261102.htm").Reply(200).BodyString(`<p>Item 2.02 Results of Operations.</p><p>See Exhibit 99.1.</p><p>SIGNATURE</p>`)
	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "ex991.htm").Reply(200).BodyString(`<p>Quarterly revenue of $114.5 billion, up 12 percent.</p>`)
}

func TestAIFilingSummary(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)
	mockFilingDocuments()
	var sent map[string]any
	gock.New("https://model.example").Post("/v1/chat/completions").MatchHeader("Authorization", "Bearer "+aiValuationKey).
		AddMatcher(func(r *http.Request, _ *gock.Request) (bool, error) {
			data, err := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(data))
			if err == nil {
				err = json.Unmarshal(data, &sent)
			}
			return err == nil, err
		}).Reply(200).JSON(map[string]any{"model": "served", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": `{"sections":[{"text":"季度营收 1145 亿美元，同比增长 12%。","refs":["Item 2.02","Exhibit 99.1"]}]}`}}}})
	w := aiFilingRequest(s, `{"url":"`+desktopFilingURL+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result llm.FilingSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "0000320193-26-000024", result.DocumentID)
	require.Equal(t, "first", result.Profile)
	require.Equal(t, "served", result.Model)
	require.True(t, result.Check.OK(), result.Check)
	require.Equal(t, []llm.FilingSource{{Label: "Item 2.02", URL: desktopFilingURL}, {Label: "Exhibit 99.1", URL: "https://www.sec.gov" + desktopFilingFolder + "ex991.htm"}}, result.Sources)
	require.NotContains(t, w.Body.String(), aiValuationKey)
	require.NotContains(t, sent, "tools")
	require.Contains(t, sent["messages"].([]any)[1].(map[string]any)["content"], "114.5 billion")

	// 与 CLI 共用缓存，命中时不请求 EDGAR 和模型。
	w = aiFilingRequest(s, `{"url":"`+desktopFilingURL+`","profile":"first"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Cached)
	require.True(t, gock.IsDone())
}

func TestAIFilingRejectsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)
	for _, body := range []string{`{}`, `null`, `{"url":"https://evil.test/a.htm"}`, `{"url":"` + desktopFilingURL + `","text":"forged"}`, `{"url":"https://www.sec.gov/Archives/edgar/data/320193/a.htm"}`} {
		require.Equal(t, http.StatusBadRequest, aiFilingRequest(s, body).Code, body)
	}
	require.Equal(t, http.StatusConflict, aiFilingRequest(s, `{"url":"`+desktopFilingURL+`","profile":"missing"}`).Code)
	s.dev, s.token = false, "secret"
	require.Equal(t, http.StatusForbidden, aiFilingRequest(s, `{"url":"`+desktopFilingURL+`"}`).Code)
	require.Empty(t, gock.GetUnmatchedRequests(), "无效参数和配置不请求 EDGAR 或模型")
}

func TestAIFilingErrors(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)

	mockFilingIndex("10-K")
	w := aiFilingRequest(s, `{"url":"`+desktopFilingURL+`"}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "目前只支持摘要 8-K")

	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "0000320193-26-000024-index.htm").Reply(500)
	w = aiFilingRequest(s, `{"url":"`+desktopFilingURL+`"}`)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "从 SEC 读取申报失败")
	require.NotContains(t, w.Body.String(), "sec.gov", "下载错误不带上游细节")

	// 申报内容的问题重试也不会成功，返回 422 和 sec 生成的原因。
	mockFilingIndex("8-K")
	gock.New("https://www.sec.gov").Get(desktopFilingFolder + "aapl-20261102.htm").Reply(200).BodyString(`<p>No items here.</p>`)
	w = aiFilingRequest(s, `{"url":"`+desktopFilingURL+`"}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "没有找到 Item 段落")

	mockFilingDocuments()
	gock.New("https://model.example").Post("/v1/chat/completions").Reply(401).BodyString(`{"error":{"message":"bad key ` + aiValuationKey + `","type":"authentication_error"}}`)
	w = aiFilingRequest(s, `{"url":"`+desktopFilingURL+`"}`)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "HTTP 401（authentication_error）")
	require.NotContains(t, w.Body.String(), aiValuationKey)
	require.True(t, gock.IsDone())
}

func TestWriteSummaryError(t *testing.T) {
	classify := func(err error) (int, string, bool) {
		if errors.Is(err, edgar.ErrDownload) {
			return http.StatusBadGateway, "下载失败", true
		}
		return 0, "", false
	}
	write := func(ctx context.Context, err error) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		writeSummaryError(w, httptest.NewRequest(http.MethodPost, "/", nil), ctx, err, "申报摘要", classify)
		return w
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	for name, c := range map[string]struct {
		ctx    context.Context
		err    error
		status int
		want   string
	}{
		"整次摘要超时":       {expired, errors.New("x"), http.StatusGatewayTimeout, "申报摘要超时"},
		"单次下载超时不算摘要超时": {context.Background(), fmt.Errorf("%w：%w", edgar.ErrDownload, context.DeadlineExceeded), http.StatusBadGateway, "下载失败"},
		"单次模型请求超时":     {context.Background(), fmt.Errorf("请求：%w", context.DeadlineExceeded), http.StatusGatewayTimeout, "模型响应超时"},
		"模型错误只给公开部分":   {context.Background(), &llm.APIError{Provider: "DeepSeek", Status: 429, Type: "rate_limit_error", Message: "sk-secret"}, http.StatusBadGateway, "DeepSeek 接口返回 HTTP 429（rate_limit_error）"},
		"其他错误":         {context.Background(), errors.New("boom"), http.StatusBadGateway, "申报摘要失败"},
	} {
		w := write(c.ctx, c.err)
		require.Equal(t, c.status, w.Code, name)
		require.Contains(t, w.Body.String(), c.want, name)
		require.NotContains(t, w.Body.String(), "sk-secret", name)
	}
	// 浏览器已取消请求时不写响应。
	r := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(expired)
	w := httptest.NewRecorder()
	writeSummaryError(w, r, expired, errors.New("x"), "申报摘要", classify)
	require.Empty(t, w.Body.String())
}
