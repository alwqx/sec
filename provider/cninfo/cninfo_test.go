package cninfo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestColumnForCode(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		// SSE main board
		{"600036", "sse"},
		{"600519", "sse"},
		{"601398", "sse"},
		{"603259", "sse"},
		// SSE STAR
		{"688001", "sse"},
		{"688981", "sse"},
		// BSE (Beijing)
		{"830000", "bj"},
		{"870000", "bj"},
		{"430000", "bj"},
		{"400000", "bj"},
		// SZSE main board
		{"000001", "szse"},
		{"001979", "szse"},
		// SZSE ChiNext
		{"300750", "szse"},
		{"301000", "szse"},
		// short / default
		{"6", "szse"},
		{"", "szse"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			require.Equal(t, tt.want, columnForCode(tt.code))
		})
	}
}

func TestPlateForCode(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		// SSE → sh
		{"600036", "sh"},
		{"688001", "sh"},
		// BSE → bj
		{"830000", "bj"},
		{"430000", "bj"},
		// SZSE (default) → sz
		{"000001", "sz"},
		{"300750", "sz"},
		{"002001", "sz"},
		// short / default → sz;sh
		{"6", "sz;sh"},
		{"", "sz;sh"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			require.Equal(t, tt.want, plateForCode(tt.code))
		})
	}
}
