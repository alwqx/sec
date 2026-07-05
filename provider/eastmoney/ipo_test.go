package eastmoney

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

/* ------------------------- parseIPOListItem ------------------------- */

func TestParseIPOListItem(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]interface{}
		want    *IListing
		wantErr bool
	}{
		{
			name: "valid item with integer listing date",
			input: map[string]interface{}{
				"f12": "920193",
				"f14": "吉和昌",
				"f26": float64(20260702),
			},
			want: &IListing{
				Code:        "920193",
				Name:        "吉和昌",
				ListingDate: "20260702",
			},
		},
		{
			name: "valid item with string listing date",
			input: map[string]interface{}{
				"f12": "300750",
				"f14": "宁德时代",
				"f26": "20180611",
			},
			want: &IListing{
				Code:        "300750",
				Name:        "宁德时代",
				ListingDate: "20180611",
			},
		},
		{
			name: "item with - listing date (forthcoming)",
			input: map[string]interface{}{
				"f12": "920222",
				"f14": "某新股",
				"f26": "-",
			},
			want: &IListing{
				Code:        "920222",
				Name:        "某新股",
				ListingDate: "-",
			},
		},
		{
			name: "empty code returns error",
			input: map[string]interface{}{
				"f12": "",
				"f14": "invalid",
			},
			wantErr: true,
		},
		{
			name: "code = - returns error",
			input: map[string]interface{}{
				"f12": "-",
				"f14": "invalid",
			},
			wantErr: true,
		},
		{
			name: "missing listing date field",
			input: map[string]interface{}{
				"f12": "600036",
				"f14": "招商银行",
			},
			want: &IListing{
				Code:        "600036",
				Name:        "招商银行",
				ListingDate: "",
			},
		},
		{
			name: "item carrying purchase code / name (kept for forward-compat)",
			input: map[string]interface{}{
				"f12": "688806",
				"f14": "泰诺麦博",
				"f26": float64(20260701),
				"f57": "787806",
				"f58": "泰诺麦博申购",
			},
			want: &IListing{
				Code:         "688806",
				Name:         "泰诺麦博",
				ListingDate:  "20260701",
				PurchaseCode: "787806",
				PurchaseName: "泰诺麦博申购",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIPOListItem(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

/* ------------------------- str / float field helpers ------------------------- */

func TestGetStrField(t *testing.T) {
	m := map[string]interface{}{
		"str":   "hello",
		"dash":  "-",
		"empty": "",
		"num":   float64(42),
		"zero":  float64(0),
	}

	require.Equal(t, "hello", getStrField(m, "str"))
	require.Equal(t, "-", getStrField(m, "dash"))
	require.Equal(t, "-", getStrField(m, "empty"))
	require.Equal(t, "42", getStrField(m, "num"))
	require.Equal(t, "-", getStrField(m, "zero"))
	require.Equal(t, "", getStrField(m, "missing"))
}

func TestGetFloat64Field(t *testing.T) {
	m := map[string]interface{}{
		"f":    float64(123.45),
		"i":    int64(7),
		"i2":   int(8),
		"s":    "3.14",
		"dash": "-",
		"bad":  "not-a-number",
	}

	require.InDelta(t, 123.45, getFloat64Field(m, "f"), 1e-9)
	require.InDelta(t, 7.0, getFloat64Field(m, "i"), 1e-9)
	require.InDelta(t, 8.0, getFloat64Field(m, "i2"), 1e-9)
	require.InDelta(t, 3.14, getFloat64Field(m, "s"), 1e-9)
	require.InDelta(t, -1.0, getFloat64Field(m, "dash"), 1e-9)
	require.InDelta(t, -1.0, getFloat64Field(m, "bad"), 1e-9)
	require.InDelta(t, -1.0, getFloat64Field(m, "missing"), 1e-9)
}

func TestListingDateString2Float(t *testing.T) {
	f, ok := listingDateString2Float("20260702")
	require.True(t, ok)
	require.InDelta(t, 20260702.0, f, 1e-9)

	_, ok = listingDateString2Float("not-a-date")
	require.False(t, ok)
}

/* ------------------------- fetchIPOListBatch with mock server ------------------------- */

const sampleIPOListJSON = `{
    "rc": 0,
    "rt": 6,
    "svr": 183120374,
    "lt": 1,
    "full": 1,
    "dlmkts": "",
    "dsc": "0",
    "data": {
        "total": 3,
        "diff": [
            {"f12": "920193", "f14": "吉和昌", "f26": 20260702, "f57": "787193", "f58": "吉和昌申购"},
            {"f12": "920222", "f14": "益坤电气", "f26": 20260630, "f57": "-", "f58": "-"},
            {"f12": "920072", "f14": "科莱瑞迪", "f26": 20260629}
        ]
    }
}`

func newTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	return srv
}

func TestFetchIPOListBatch(t *testing.T) {
	srv := newTestServer(t, sampleIPOListJSON)
	defer srv.Close()

	// 用 httptest URL 替换 ipoURL（包级变量，测试结束恢复）
	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	items, total, err := fetchIPOListBatch(context.Background(), 1, 30)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	require.Len(t, items, 3)

	require.Equal(t, "920193", items[0].Code)
	require.Equal(t, "吉和昌", items[0].Name)
	require.Equal(t, "20260702", items[0].ListingDate)
	require.Equal(t, "787193", items[0].PurchaseCode)
	require.Equal(t, "吉和昌申购", items[0].PurchaseName)

	require.Equal(t, "920072", items[2].Code)
	require.Equal(t, "20260629", items[2].ListingDate)
}

func TestFetchIPOListBatchServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	// 无效 JSON / 错误 status 会走到 Unmarshal 失败
	_, _, err := fetchIPOListBatch(context.Background(), 1, 30)
	require.Error(t, err)
}

/* ------------------------- ListIPO (multi-page + max total) ------------------------- */

// newMultiPageServer 返回 total=5、每页 3 条的 fake server
func newMultiPageServer(t *testing.T) *httptest.Server {
	t.Helper()
	responseForPage := func(page int) string {
		start := []map[string]interface{}{
			{"f12": "001", "f14": "新股1", "f26": float64(20260701)},
			{"f12": "002", "f14": "新股2", "f26": float64(20260630)},
			{"f12": "003", "f14": "新股3", "f26": float64(20260629)},
		}
		if page == 1 {
			_ = start
		} else {
			start = []map[string]interface{}{
				{"f12": "004", "f14": "新股4", "f26": float64(20260628)},
				{"f12": "005", "f14": "新股5", "f26": float64(20260627)},
			}
		}
		total := 5
		if page > 1 {
			total = 0 // 仅第一页返回 total
		}
		data := map[string]interface{}{
			"rc": 0,
			"data": map[string]interface{}{
				"total": total,
				"diff":  start,
			},
		}
		b, _ := json.Marshal(data)
		return string(b)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pn := r.URL.Query().Get("pn")
		resp := responseForPage(1)
		if pn == "2" || pn == "02" {
			resp = responseForPage(2)
		}
		_, _ = io.WriteString(w, resp)
	}))
	return srv
}

func TestListIPO_OnePage(t *testing.T) {
	// mock server：第一页 3 条，第二页 2 条（第一页 total=5）。
	// MaxTotal=10 > 3 → 第一页满后继续拉第二页，共 5 条。
	srv := newMultiPageServer(t)
	defer srv.Close()

	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	items, total, err := ListIPO(context.Background(), &IPOListReq{
		PageNum:  1,
		PageSize: 3,
		MaxTotal: 10,
	})
	require.NoError(t, err)
	require.Equal(t, 5, total) // 实现返回 total=0；保持行为稳定
	require.Len(t, items, 5)   // 拉满两页（3 + 2）
}

func TestListIPO_MaxTotalCapped(t *testing.T) {
	srv := newMultiPageServer(t)
	defer srv.Close()

	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	items, _, err := ListIPO(context.Background(), &IPOListReq{
		PageNum:  1,
		PageSize: 3,
		MaxTotal: 2,
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
}

func TestListIPO_NilReq(t *testing.T) {
	srv := newMultiPageServer(t)
	defer srv.Close()

	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	items, _, err := ListIPO(context.Background(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, items)
}

func TestListIPO_SinglePageShortBatch(t *testing.T) {
	// mock server（pageSize=30 但服务器仅返回 3 条）→ ListIPO 在 len(batch) < pageSize 时退出
	body := `{
		"rc": 0,
		"data": {
			"total": 3,
			"diff": [
				{"f12": "001", "f14": "新股1", "f26": 20260701},
				{"f12": "002", "f14": "新股2", "f26": 20260630},
				{"f12": "003", "f14": "新股3", "f26": 20260629}
			]
		}
	}`
	srv := newTestServer(t, body)
	defer srv.Close()

	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	items, _, err := ListIPO(context.Background(), &IPOListReq{
		PageNum:  1,
		PageSize: 30,
		MaxTotal: 0,
	})
	require.NoError(t, err)
	require.Len(t, items, 3) // 服务端返回 3 条即退出（len(batch) < pageSize）
}

/* ------------------------- fetchIPOListBatch handling empty / malformed body ------------------------- */

func TestFetchIPOListBatchEmptyDiff(t *testing.T) {
	body := `{"rc":0,"data":{"total":0,"diff":[]}}`
	srv := newTestServer(t, body)
	defer srv.Close()

	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	items, total, err := fetchIPOListBatch(context.Background(), 1, 30)
	require.NoError(t, err)
	require.Equal(t, 0, total)
	require.Empty(t, items)
}

func TestFetchIPOListBatchSkipsInvalidItems(t *testing.T) {
	body := `{
		"rc": 0,
		"data": {
			"total": 3,
			"diff": [
				{"f12": "-", "f14": "bad", "f26": "-"},
				{"f12": "",  "f14": "bad2"},
				{"f12": "123456", "f14": "good", "f26": "20260702"}
			]
		}
	}`
	srv := newTestServer(t, body)
	defer srv.Close()

	origURL := ipoURL
	ipoURL = srv.URL + "/api/qt/clist/get"
	defer func() { ipoURL = origURL }()

	items, _, err := fetchIPOListBatch(context.Background(), 1, 30)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "123456", items[0].Code)
}

/* ------------------------- URL query parameters (sanity) ------------------------- */

func TestIPOListReq_URLFields(t *testing.T) {
	// 直接构造一个 fetchIPOListBatch 使用的 url.Values，
	// 校验关键查询参数是否正确。
	v := url.Values{}
	v.Set("pn", "1")
	v.Set("pz", "30")
	v.Set("np", "1")
	v.Set("fltt", "2")
	v.Set("invt", "2")
	v.Set("fid", ipoSortField)
	v.Set("po", "1")
	v.Set("fs", ipoFS)
	v.Set("fields", ipoFields)

	require.Equal(t, "f26", v.Get("fid"))
	require.Equal(t, "1", v.Get("po"))
	require.Equal(t, "m:0+t:80+t:81+s:2048", v.Get("fs"))
	require.NotEmpty(t, v.Get("fields"))
	require.Contains(t, v.Get("fields"), "f12")
	require.Contains(t, v.Get("fields"), "f26")
	require.Contains(t, v.Get("fields"), "f14")
}
