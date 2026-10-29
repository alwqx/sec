package fred

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 从真实档案保留其他结构，仅修改需要验证的源单元格，避免构造与真实格式无关的测试表。
func mutateGDPWorkbook(t *testing.T, raw []byte, mutate func(string, string) string) []byte {
	t.Helper()
	original, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range original.File {
		reader, err := file.Open()
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		entry, err := writer.Create(file.Name)
		require.NoError(t, err)
		_, err = entry.Write([]byte(mutate(file.Name, string(data))))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return output.Bytes()
}

func TestGDPVintageRejectsWrongColumnsAndDate(t *testing.T) {
	raw, err := os.ReadFile("testdata/us-gdp-vintages.xlsx")
	require.NoError(t, err)
	for _, test := range []struct{ name, from, to string }{
		{"名义金额列不能默认为实际金额", "Billions of Current Dollars", "Billions of Chained 2017 Dollars"},
		{"实际增速列不能读取GDI", "<t>Real GDP</t>", "<t>Real GDI</t>"},
		{"错误发布日期不能缓存", "Sep 30, 2026", "Sep 31, 2026"},
		{"不混入声明覆盖起点前的季度", "<t>2026Q2</t>", "<t>2001Q4</t>"},
	} {
		t.Run(test.name, func(t *testing.T) {
			mutated := mutateGDPWorkbook(t, raw, func(name, body string) string {
				if name == "xl/sharedStrings.xml" {
					require.Contains(t, body, test.from)
					return strings.Replace(body, test.from, test.to, 1)
				}
				return body
			})
			_, err := parseGDPVintages(mutated)
			require.Error(t, err)
		})
	}
}

func TestGDPVintageSignedGrowthAndMissingRealLevel(t *testing.T) {
	// 实际增长率为负或为零均有效，只有金额层级禁止负数。
	raw, err := os.ReadFile("testdata/us-gdp-vintages.xlsx")
	require.NoError(t, err)
	values, err := parseGDPVintages(raw)
	require.NoError(t, err)
	negative := false
	for _, records := range values {
		for _, record := range records {
			require.Nil(t, record.RealLevel)
			if record.QoQAnnualized != nil {
				negative = negative || *record.QoQAnnualized < 0
			}
		}
	}
	require.True(t, negative)
	mutated := mutateGDPWorkbook(t, raw, func(name, body string) string {
		if name == "xl/sharedStrings.xml" {
			return strings.Replace(body, "<t>1.5</t>", "<t>0.0</t>", 1)
		}
		return body
	})
	zeroValues, err := parseGDPVintages(mutated)
	require.NoError(t, err)
	require.Equal(t, 0.0, *zeroValues["2026Q2"][0].QoQAnnualized)
}
