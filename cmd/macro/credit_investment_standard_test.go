package macro

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestCreditInvestmentStandardErrors(t *testing.T) {
	for _, test := range []struct {
		name, begin, end, file, id string
		options                    []string
	}{
		{"bank-credit", "202609", "202610", "us-credit-bank", "LOANINV", nil},
		{"lending-standards", "2026Q3", "2026Q4", "us-credit-standards", "DRTSCILM", nil},
		{"debt-service", "2026Q2", "2026Q3", "us-credit-debt-service", "TDSP", nil},
		{"investment", "202609", "202610", "us-investment-housing", "HOUST,PERMIT", []string{"--component", "housing-starts"}},
		{"investment", "202609", "202610", "us-investment-housing", "HOUST,PERMIT", []string{"--component", "building-permits"}},
		{"equipment-investment", "2026Q2", "2026Q3", "us-investment-equipment", "Y033RC1Q027SBEA,Y033RX1Q020SBEA", []string{"--price-basis", "real"}},
		{"equipment-investment", "2026Q2", "2026Q3", "us-investment-equipment", "Y033RC1Q027SBEA,Y033RX1Q020SBEA", []string{"--price-basis", "nominal"}},
	} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			t.Run(test.name+"/"+test.file+"/"+mode+"/"+test.id+"/"+strings.Join(test.options, "_"), func(t *testing.T) {
				defer gock.Off()
				args := []string{"us", test.name, "--begin", test.begin, "--end", test.end, "--standard-json", "--no-cache"}
				args = append(args, test.options...)
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "http":
					gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", test.id).Reply(503)
				case "writer":
					raw, err := os.ReadFile("../../provider/fred/testdata/" + test.file + ".csv")
					require.NoError(t, err)
					gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", test.id).Reply(200).BodyString(string(raw))
				}
				cmd := NewMacroCLI()
				cmd.SetArgs(args)
				var out bytes.Buffer
				cmd.SetOut(&out)
				if mode == "writer" {
					cmd.SetOut(failingMoneyWriter{})
				}
				cmd.SetErr(&bytes.Buffer{})
				cmd.SilenceUsage = true
				err := cmd.ExecuteContext(ctx)
				require.Error(t, err)
				switch mode {
				case "flags":
					require.ErrorContains(t, err, "none of the others can be")
				case "cancel":
					require.ErrorIs(t, err, context.Canceled)
				case "http":
					require.ErrorContains(t, err, "503")
				case "writer":
					require.ErrorContains(t, err, "测试写入失败")
				}
				if mode != "writer" {
					require.Empty(t, out.String())
				}
				require.True(t, gock.IsDone())
			})
		}
	}
}
