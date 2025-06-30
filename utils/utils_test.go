package utils

import (
	"io"
	"net/http"
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
