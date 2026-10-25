//go:build windows || darwin

package main

import (
	"github.com/alwqx/sec/version"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
	"net/http/httptest"
	"testing"
)

func TestVersionAndUpdates(t *testing.T) {
	previousVersion, previousTime, previousCommit := version.Version, version.BuildTime, version.GitCommit
	defer func() {
		version.Version, version.BuildTime, version.GitCommit = previousVersion, previousTime, previousCommit
	}()
	version.Version, version.BuildTime, version.GitCommit = "v0.3.9", "2026-10-25T10:00:00Z", "abc1234"
	s := &desktopServer{dev: true}
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/version", nil))
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), "abc1234")
	require.Contains(t, response.Body.String(), version.BuildTime)
	defer gock.Off()
	for _, tc := range []struct{ current, status string }{{"v0.3.9", "available"}, {"v0.3.10", "current"}, {"v0.4.0", "ahead"}, {"", "unknown"}} {
		version.Version = tc.current
		gock.New("https://api.github.com").Get("/repos/alwqx/sec/releases/latest").Reply(200).BodyString(`{"tag_name":"v0.3.10","assets":[]}`)
		response = httptest.NewRecorder()
		s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/updates", nil))
		require.Equal(t, 200, response.Code)
		require.Contains(t, response.Body.String(), `"status":"`+tc.status+`"`)
		require.Contains(t, response.Body.String(), `"asset":null`)
	}
	gock.New("https://api.github.com").Get("/repos/alwqx/sec/releases/latest").Reply(403)
	response = httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/updates", nil))
	require.Equal(t, 502, response.Code)
	for _, endpoint := range []string{"version", "updates"} {
		response = httptest.NewRecorder()
		(&desktopServer{token: "secret"}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/"+endpoint, nil))
		require.Equal(t, 403, response.Code)
	}
}
