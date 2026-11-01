package fx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExportArgs(t *testing.T) {
	for name, test := range map[string]struct {
		query ExportQuery
		want  []string
	}{
		"quote ignores dates":     {ExportQuery{Command: "quote", Begin: "20260101", Refresh: true}, []string{"quote", "--json", "--refresh"}},
		"history default source":  {ExportQuery{Command: "history", Begin: "20260101", End: "20260131"}, []string{"history", "--json", "--source=sina", "--begin=20260101", "--end=20260131"}},
		"fixing ignores source":   {ExportQuery{Command: "fixing", Source: "fred", At: "16:00"}, []string{"fixing", "--json"}},
		"reference at":            {ExportQuery{Command: "reference", At: "10:00"}, []string{"reference", "--json", "--at=10:00"}},
		"cross fred ignores at":   {ExportQuery{Command: "cross", Source: "fred", Pair: "HKD/CNY", At: "10:00", MaxAge: "1h"}, []string{"cross", "--json", "--source=fred", "--pairs=HKD/CNY"}},
		"cross cfets at":          {ExportQuery{Command: "cross", Source: "cfets", Pair: "USD/HKD", At: "10:00"}, []string{"cross", "--json", "--source=cfets", "--pairs=USD/HKD", "--at=10:00"}},
		"cross sina ignores date": {ExportQuery{Command: "cross", Source: "sina", Pair: "HKD/CNH", Begin: "20260101", MaxSkew: "60s", MaxAge: "1h"}, []string{"cross", "--json", "--source=sina", "--pairs=HKD/CNH", "--max-skew=60s", "--max-age=1h"}},
		"spread policy":           {ExportQuery{Command: "spread", Begin: "20260101", MaxSkew: "60s", MaxAge: "24h"}, []string{"spread", "--json", "--max-skew=60s", "--max-age=24h"}},
	} {
		t.Run(name, func(t *testing.T) {
			args, err := exportArgs(test.query)
			require.NoError(t, err)
			require.Equal(t, test.want, args)
		})
	}
}

// 生成的参数必须是命令实际接受的；否则页面会在运行时才报错。
func TestExportArgsParseByCommand(t *testing.T) {
	for _, query := range []ExportQuery{
		{Command: "quote"}, {Command: "history", Source: "fred"}, {Command: "fixing"}, {Command: "reference", At: "10:00"},
		{Command: "cross", Source: "cfets", Pair: "USD/HKD", At: "10:00"}, {Command: "cross", Source: "sina", Pair: "HKD/CNH", MaxSkew: "60s", MaxAge: "1h"},
		{Command: "spread", MaxSkew: "60s", MaxAge: "24h"},
	} {
		args, err := exportArgs(query)
		require.NoError(t, err)
		command, flags, err := NewFXCLI().Find(args)
		require.NoError(t, err)
		require.NoError(t, command.ParseFlags(flags), args)
	}
}

func TestExportRejectsInvalidQuery(t *testing.T) {
	for name, query := range map[string]ExportQuery{
		"command":   {Command: "nope"},
		"flag":      {Command: "--json"},
		"source":    {Command: "history", Source: "cfets"},
		"date":      {Command: "fixing", Begin: "2026"},
		"calendar":  {Command: "fixing", End: "20260230"},
		"reversed":  {Command: "fixing", Begin: "20260201", End: "20260101"},
		"pair":      {Command: "cross", Pair: "--json"},
		"no pair":   {Command: "cross", Source: "fred", Pair: "CNH/USD"},
		"at":        {Command: "reference", At: "16:30"},
		"max skew":  {Command: "spread", MaxSkew: "1h"},
		"max age":   {Command: "spread", MaxAge: "48h"},
		"sina skew": {Command: "cross", Source: "sina", MaxSkew: "5s"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Export(context.Background(), query)
			require.ErrorIs(t, err, ErrInvalidQuery)
		})
	}
}

func TestExportReturnsCommandErrors(t *testing.T) {
	_, err := Export(context.Background(), ExportQuery{Command: "fixing", Begin: "20000101", End: "20000201"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidQuery)
}
