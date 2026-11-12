package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/internal/testpdf"
	"github.com/alwqx/sec/llm"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const desktopSummaryURL = "https://static.cninfo.com.cn/finalpage/2026-11-01/123456.pdf"

func aiSummaryRequest(s *desktopServer, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/announcements/summary", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func mockSummaryPDF() {
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-11-01/123456.pdf").Reply(200).
		Body(bytes.NewReader(testpdf.Build("收入 1200 万元。报告期 2025 年。", "风险：合同尚需审批。")))
}

func TestAIAnnouncementSummary(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	path := configureAIValuation(t, s)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	mockSummaryPDF()
	var sent map[string]any
	gock.New("https://model.example").Post("/v1/chat/completions").MatchHeader("Authorization", "Bearer "+aiValuationKey).
		AddMatcher(func(r *http.Request, _ *gock.Request) (bool, error) {
			data, err := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(data))
			if err == nil {
				err = json.Unmarshal(data, &sent)
			}
			return err == nil, err
		}).Reply(200).JSON(map[string]any{"model": "served", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": `{"sections":[{"text":"收入 1200 万元，报告期 2025 年。","pages":[1]},{"text":"合同尚需审批。","pages":[2]}]}`}}}})
	w := aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result llm.AnnouncementSummary
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "first", result.Profile)
	require.Equal(t, "served", result.Model)
	require.Equal(t, "2026-11-01", result.AsOf)
	require.Equal(t, 2, result.PageCount)
	require.True(t, result.Check.OK(), result.Check)
	require.Contains(t, result.Text, "PDF 第 2 页")
	require.NotContains(t, w.Body.String(), aiValuationKey)
	require.NotContains(t, sent, "tools")
	require.Contains(t, sent["messages"].([]any)[1].(map[string]any)["content"], "合同尚需审批")
	w = aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`","profile":"first"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Cached, "与 CLI 使用同一份缓存，命中不下载或调用模型")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "摘要不修改配置或 default")
	require.True(t, gock.IsDone())
}

func TestAIAnnouncementRejectsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	path := configureAIValuation(t, s)
	for _, body := range []string{`{}`, `null`, `{} {}`, `{"url":"https://evil.test/file.pdf"}`, `{"url":"` + desktopSummaryURL + `","text":"forged"}`, `{"url":"` + desktopSummaryURL + `#page=1"}`} {
		require.Equal(t, http.StatusBadRequest, aiSummaryRequest(s, body).Code, body)
	}
	require.Equal(t, http.StatusConflict, aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`","profile":"missing"}`).Code)
	require.NoError(t, config.UpdateAIFile(context.Background(), path, func(ai *config.AIConfig) error { return ai.SetEnabled(false) }))
	require.Equal(t, http.StatusConflict, aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`"}`).Code)
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{"enabled":"yes"}}`), 0o600))
	require.Equal(t, http.StatusConflict, aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`"}`).Code)
	s.dev, s.token = false, "secret"
	require.Equal(t, http.StatusForbidden, aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`"}`).Code)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/announcements/summary", strings.NewReader(`{"url":"`+desktopSummaryURL+`"}`))
	r.AddCookie(&http.Cookie{Name: "token", Value: "secret"})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://external.example")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Empty(t, gock.GetUnmatchedRequests())
}

func TestAIAnnouncementModelAndPDFErrors(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)
	mockSummaryPDF()
	gock.New("https://model.example").Post("/v1/chat/completions").Reply(401).
		BodyString(`{"error":{"message":"bad key ` + aiValuationKey + `"}}`)
	w := aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`"}`)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "HTTP 401")
	require.NotContains(t, w.Body.String(), aiValuationKey)
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-11-01/123456.pdf").Reply(200).BodyString("<html>not pdf</html>")
	w = aiSummaryRequest(s, `{"url":"`+desktopSummaryURL+`"}`)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "扫描件")
	require.True(t, gock.IsDone())
}
