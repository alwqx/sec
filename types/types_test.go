package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHumanNum(t *testing.T) {
	require.EqualValues(t, " - ", HumanNum(-1))
	require.EqualValues(t, " - ", HumanNum(0.0))
	require.EqualValues(t, "1.00万", HumanNum(10000))
	require.EqualValues(t, "10.09万", HumanNum(100900))
	require.EqualValues(t, "1000.09亿", HumanNum(100009000009))
}

func TestIsACode(t *testing.T) {
	testCode := []struct {
		Name   string
		ExCode string
		Res    bool
	}{
		{
			Name:   "1 empty excode",
			ExCode: "",
		},
		{
			Name:   "2 common sh",
			ExCode: "SH600036",
			Res:    true,
		},
		{
			Name:   "2.1 common sz",
			ExCode: "SZ002475",
			Res:    true,
		},
		{
			Name:   "2.2 common bj",
			ExCode: "BJ834475",
			Res:    true,
		},
		{
			Name:   "3 uncommon code",
			ExCode: "xxx834475",
			Res:    false,
		},
		{
			Name:   "4 hk code",
			ExCode: "HK00700",
			Res:    false,
		},
	}

	for _, tc := range testCode {
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Res, IsACode(tc.ExCode))
		})
	}
}

func TestIsHCode(t *testing.T) {
	testCode := []struct {
		Name   string
		ExCode string
		Res    bool
	}{
		{
			Name:   "1 empty excode",
			ExCode: "",
		},
		{
			Name:   "2 common sh",
			ExCode: "SH600036",
			Res:    false,
		},
		{
			Name:   "2.1 common sz",
			ExCode: "SZ002475",
			Res:    false,
		},
		{
			Name:   "2.2 common bj",
			ExCode: "BJ834475",
			Res:    false,
		},
		{
			Name:   "3 uncommon code",
			ExCode: "xxx834475",
			Res:    false,
		},
		{
			Name:   "4 hk code",
			ExCode: "HK00700",
			Res:    true,
		},
	}

	for _, tc := range testCode {
		t.Run(tc.Name, func(t *testing.T) {
			require.Equal(t, tc.Res, IsHCode(tc.ExCode))
		})
	}
}
