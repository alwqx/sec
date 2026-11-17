package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alwqx/sec/internal/testpdf"
	"github.com/alwqx/sec/llm"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const desktopProspectusURL = "https://static.cninfo.com.cn/finalpage/2025-11-28/1224820000.PDF"

func aiProspectusRequest(s *desktopServer, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/prospectus/ask", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func mockProspectusPDF() {
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2025-11-28/1224820000.PDF").Reply(200).
		Body(bytes.NewReader(testpdf.Build("保荐人 声明", "实际控制人 张建中", "募集资金运用 新一代 GPU 研发项目")))
}

func mockProspectusAnswer(text string, page int) {
	content, _ := json.Marshal(map[string]any{"sections": []any{map[string]any{"text": text, "pages": []int{page}}}})
	gock.New("https://model.example").Post("/v1/chat/completions").Reply(200).
		JSON(map[string]any{"model": "served", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(content)}}}})
}

func TestAIProspectusAsk(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)
	mockProspectusPDF()
	mockProspectusAnswer("实际控制人是张建中。", 2)
	w := aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":" 实际控制人是谁？ "}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result llm.ProspectusAnswer
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "实际控制人是谁？", result.Question)
	require.Equal(t, "first", result.Profile)
	require.Equal(t, 3, result.PageCount)
	require.Equal(t, []int{2, 3}, result.Pages, "挑中的页带上下一页")
	require.Contains(t, result.Text, "PDF 第 2 页")
	require.NotContains(t, w.Body.String(), aiValuationKey)

	w = aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":"实际控制人是谁？","profile":"first"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Cached, "与 CLI 使用同一份缓存，命中不下载或调用模型")

	// 换个问题只请求模型，招股书的页已经缓存，不再下载 PDF。
	mockProspectusAnswer("募集资金用于新一代 GPU 研发项目。", 3)
	w = aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":"募集资金运用"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.False(t, result.Cached)
	require.True(t, gock.IsDone())
}

func TestAIProspectusRejectsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)
	for _, body := range []string{`{}`, `null`, `{"url":"https://evil.test/file.pdf","question":"营业收入"}`,
		`{"url":"` + desktopProspectusURL + `"}`, `{"url":"` + desktopProspectusURL + `","question":"  "}`,
		`{"url":"` + desktopProspectusURL + `","question":"请问是什么？"}`,
		`{"url":"` + desktopProspectusURL + `","question":"` + strings.Repeat("问", 201) + `"}`,
		`{"url":"` + desktopProspectusURL + `","question":"营业收入","pages":[1]}`} {
		require.Equal(t, http.StatusBadRequest, aiProspectusRequest(s, body).Code, body)
	}
	require.Equal(t, http.StatusConflict, aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":"营业收入","profile":"missing"}`).Code)
	s.dev, s.token = false, "secret"
	require.Equal(t, http.StatusForbidden, aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":"营业收入"}`).Code)
	require.Empty(t, gock.GetUnmatchedRequests())
}

func TestAIProspectusErrors(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, s)
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2025-11-28/1224820000.PDF").Reply(200).BodyString("<html>not pdf</html>")
	w := aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":"实际控制人"}`)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "1000 页")

	mockProspectusPDF()
	w = aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":"董事会秘书"}`)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	require.Contains(t, w.Body.String(), "没有和问题相关的页")

	gock.New("https://model.example").Post("/v1/chat/completions").Reply(401).
		BodyString(`{"error":{"message":"bad key ` + aiValuationKey + `"}}`)
	w = aiProspectusRequest(s, `{"url":"`+desktopProspectusURL+`","question":"实际控制人"}`)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "HTTP 401")
	require.NotContains(t, w.Body.String(), aiValuationKey)
	require.True(t, gock.IsDone())
}
