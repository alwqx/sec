//go:build windows || darwin

package main

import (
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestIPODateValidation(t *testing.T) {
	for _, query := range []string{"since=2024-02-30", "until=bad", "since=2025-01-01&until=2024-01-01"} {
		response := httptest.NewRecorder()
		(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/ipo?"+query, nil))
		require.Equal(t, 400, response.Code)
	}
}
