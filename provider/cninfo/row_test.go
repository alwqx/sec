package cninfo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestToRows(t *testing.T) {
	// 北京时间 2026-11-01 00:30，UTC 还是前一天，不能随本机时区变化。
	at := time.Date(2026, 10, 31, 16, 30, 0, 0, time.UTC).UnixMilli()
	rows := ToRows([]*Announcement{
		nil,
		{ID: "1", Title: "<em>招商</em>银行年报", Time: at, SecCode: "600036", SecName: "招商银行", TypeName: "年报", AdjunctSize: 12, AdjunctURL: "finalpage/a.PDF"},
		{ID: "2", Title: "已失效", InvalidationFlag: 1},
		{ID: "3", Title: "不存在", ExistFlag: 1},
		{ID: "4", Title: "没有时间"},
	})
	require.Len(t, rows, 2)
	date, size := "2026-11-01", int64(12)
	require.Equal(t, Row{&date, "600036", "招商银行", "招商银行年报", "年报", &size, "https://static.cninfo.com.cn/finalpage/a.PDF"}, rows[0])
	require.Nil(t, rows[1].Date, "缺失是 null")
	require.Nil(t, rows[1].SizeKB, "缺失是 null")
	require.Equal(t, []Row{}, ToRows(nil), "结果不为 nil，输出空数组")
}

func TestDateText(t *testing.T) {
	// 北京时间 2026-11-01 00:00，本机时区比北京晚时不能变成前一天。
	at := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC).UnixMilli()
	require.Equal(t, "2026-11-01", (&Announcement{Time: at}).DateText())
	require.Empty(t, (&Announcement{}).DateText())
}

func TestSizeBytes(t *testing.T) {
	require.Equal(t, 105.0*1024, (&Announcement{AdjunctSize: 105}).SizeBytes(), "接口的单位是 KB")
}
