package utils

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/alwqx/sec/version"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUserAgent(t *testing.T) {
	version.Version = "test"
	require.NotEqual(t, "", UserAgent())
}

func TestMakeRequest(t *testing.T) {
	defer gock.Off()
	gock.New("http://abc.xyz").Get("/foo").Reply(200).JSON(`OK`)
	resp, err := MakeRequest(http.MethodGet, "http://abc.xyz/foo", nil, nil)
	require.Nil(t, err)
	require.NotNil(t, resp)

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.Nil(t, err)
	require.Equal(t, "OK", string(body))
}

func TestWriteJson(t *testing.T) {
	tmpDir := os.TempDir()
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})
	// 1. nil
	err := WriteJson(nil, tmpDir)
	require.Nil(t, err)

	// 2 data
	data := struct {
		Name string
	}{
		Name: "TestFile",
	}

	filePath := filepath.Join(tmpDir, "data.json")
	err = WriteJson(data, filePath)
	require.Nil(t, err)
}
