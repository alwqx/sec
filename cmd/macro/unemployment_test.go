package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func TestUnemploymentOutput(t *testing.T) {
	value, zero := 14.9, 0.0
	items := []nbs.UnemploymentObservation{
		{Period: "202312", Indicator: "age-16-24", Rate: &value, StudentScope: "excluded", Note: "与旧口径不直接可比"},
		{Period: "202311", Indicator: "age-16-24", StudentScope: "not-applicable", Note: "分年龄组暂停发布期"},
		{Period: "202312", Indicator: "urban-surveyed", Rate: &zero, StudentScope: "included-if-in-labour-force"},
	}
	var out bytes.Buffer
	require.NoError(t, printUnemployment(&out, items))
	for _, text := range []string{"16—24岁", "14.9", "0.0", "—", "不含在校生", "暂停发布", "与旧口径不直接可比", "非登记失业率"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	require.NoError(t, printUnemployment(&out, nil))
	require.Contains(t, out.String(), "暂无")
}

func TestUnemploymentArguments(t *testing.T) {
	for _, args := range [][]string{
		{"cn", "unemployment", "--begin", "202613"},
		{"cn", "unemployment", "--begin", "202609", "--end", "202601", "--age-groups"},
		{"cn", "unemployment", "extra"},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
