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

func TestResolvePDFURL(t *testing.T) {
	tests := []struct {
		name string
		in   *Announcement
		want string
	}{
		{
			name: "relative path (typical cninfo adjunct url)",
			in: &Announcement{
				SecCode:    "300750",
				ID:         "1205010303",
				OrgID:      "GD165627",
				AdjunctURL: "finalpage/2018-05-29/1205010303.PDF",
			},
			want: "http://static.cninfo.com.cn/finalpage/2018-05-29/1205010303.PDF",
		},
		{
			name: "relative path with leading slash",
			in: &Announcement{
				SecCode:    "600036",
				ID:         "123",
				OrgID:      "gssh0600036",
				AdjunctURL: "/finalpage/2024-01-01/123.PDF",
			},
			want: "http://static.cninfo.com.cn/finalpage/2024-01-01/123.PDF",
		},
		{
			name: "absolute http url returned as-is",
			in: &Announcement{
				SecCode:    "000001",
				ID:         "456",
				OrgID:      "9900000001",
				AdjunctURL: "http://other-cdn.example.com/path/doc.pdf",
			},
			want: "http://other-cdn.example.com/path/doc.pdf",
		},
		{
			name: "absolute https url returned as-is",
			in: &Announcement{
				SecCode:    "000001",
				ID:         "456",
				OrgID:      "9900000001",
				AdjunctURL: "https://static.cninfo.com.cn/finalpage/2024-01-01/456.PDF",
			},
			want: "https://static.cninfo.com.cn/finalpage/2024-01-01/456.PDF",
		},
		{
			name: "empty adjunct url falls back to detail page",
			in: &Announcement{
				SecCode: "300750",
				ID:      "1205010303",
				OrgID:   "GD165627",
			},
			want: "https://www.cninfo.com.cn/new/disclosure/detail?stockCode=300750&announcementId=1205010303&orgId=GD165627",
		},
		{
			name: "empty adjunct url with empty fields",
			in:   &Announcement{},
			want: "https://www.cninfo.com.cn/new/disclosure/detail?stockCode=&announcementId=&orgId=",
		},
		{
			name: "single-slash relative path",
			in: &Announcement{
				SecCode:    "600036",
				ID:         "789",
				OrgID:      "gssh0600036",
				AdjunctURL: "/abc.pdf",
			},
			want: "http://static.cninfo.com.cn/abc.pdf",
		},
		{
			name: "adjunct url that starts with http but lower-case",
			in: &Announcement{
				SecCode:    "600036",
				ID:         "789",
				OrgID:      "gssh0600036",
				AdjunctURL: "http:/malformed/doc.pdf",
			},
			// Starts with "http" but lacks the second slash; current impl treats any
			// "http"-prefixed url as absolute (Prefix match). Document the behavior.
			want: "http:/malformed/doc.pdf",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvePDFURL(tt.in)
			require.Equal(t, tt.want, got)
		})
	}
}
