package release

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestFetchLatestAndDesktopAsset(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.github.com").Get("/repos/alwqx/sec/releases/latest").Reply(200).JSON(map[string]any{
		"tag_name": "v0.3.20", "assets": []Asset{
			{Name: "sec-v0.3.20-darwin-arm64.tar.gz", BrowserDownloadURL: "https://github.com/alwqx/sec/releases/download/v0.3.20/cli.tar.gz"},
			{Name: "sec-v0.3.20-desktop-darwin-arm64.dmg", BrowserDownloadURL: "https://github.com/alwqx/sec/releases/download/v0.3.20/sec-v0.3.20-desktop-darwin-arm64.dmg", Size: 1024},
		}})
	info, err := FetchLatest(context.Background())
	require.NoError(t, err)
	require.Equal(t, "v0.3.20", info.TagName)
	require.NotNil(t, DesktopAsset(info, "darwin", "arm64"))
	require.Nil(t, DesktopAsset(info, "darwin", "amd64"))
	require.Nil(t, DesktopAsset(info, "windows", "arm64"))
	info.Assets[1].BrowserDownloadURL = "https://example.com/install.dmg"
	require.Nil(t, DesktopAsset(info, "darwin", "arm64"))
}

func TestReleaseFailures(t *testing.T) {
	defer gock.Off()
	for _, tc := range []struct {
		status int
		body   string
	}{{403, `{}`}, {500, `{}`}, {200, `broken`}, {200, `{"tag_name":""}`}} {
		gock.New("https://api.github.com").Get("/repos/alwqx/sec/releases/latest").Reply(tc.status).BodyString(tc.body)
		_, err := FetchLatest(context.Background())
		require.Error(t, err)
	}
}

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		order           int
		known           bool
	}{
		{"v0.3.9", "v0.3.10", -1, true}, {"0.3.10", "v0.3.10", 0, true}, {"v0.4.0", "v0.3.10", 1, true},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1, true}, {"v1.0.0-rc.1", "v1.0.0", -1, true}, {"v1.0.0+build1", "v1.0.0+build2", 0, true},
		{"", "v1.0.0", 0, false}, {"abcdef1", "v1.0.0", 0, false}, {"v1.0.0-01", "v1.0.0", 0, false},
	} {
		order, known := Compare(tc.current, tc.latest)
		require.Equal(t, tc.known, known, tc)
		require.Equal(t, tc.order, order, tc)
	}
}

func TestReleaseURLValidation(t *testing.T) {
	for _, raw := range []string{LatestURL, PageURL("v0.3.20"), "https://github.com/alwqx/sec/releases/download/v0.3.20/app.dmg"} {
		require.NoError(t, ValidateURL(raw))
	}
	for _, raw := range []string{"http://github.com/alwqx/sec/releases/latest", "https://github.com@evil.example/alwqx/sec/releases/latest", "https://github.com/other/repo/releases/latest", LatestURL + "?redirect=evil", "https://github.com/alwqx/sec/releases/tag/../../other", "javascript:alert(1)"} {
		require.Error(t, ValidateURL(raw))
	}
}
