package cninfo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProspectusKind(t *testing.T) {
	for title, want := range map[string]string{
		"摩尔线程首次公开发行股票并在科创板上市招股说明书 ":       KindProspectus,
		"摩尔线程首次公开发行股票并在科创板上市招股说明书（注册稿）":   KindProspectus,
		"某公司招股意向书(更新后)":                   KindPreliminary,
		"摩尔线程首次公开发行股票并在科创板上市招股意向书":        KindPreliminary,
		"摩尔线程首次公开发行股票并在科创板上市招股说明书提示性公告":   "",
		"摩尔线程首次公开发行股票并在科创板上市招股意向书附录":      "",
		"摩尔线程首次公开发行股票并在科创板上市招股说明书摘要（修订稿）": "",
		"首次公开发行股票发行公告":                    "",
		"某公司招股说明书（注册稿）（更新后）":              KindProspectus,
		"某公司<em>招股说明书</em>":               KindProspectus,
		"某公司招股意向书&#xff08;更新后&#xff09;":     KindPreliminary,
	} {
		require.Equal(t, want, ProspectusKind(title), title)
	}
}
