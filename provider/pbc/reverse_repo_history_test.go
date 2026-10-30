package pbc

import (
	"context"
	_ "embed"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

//go:embed testdata/reverse-repo-history.html
var reverseRepoHistoryFixture []byte

func TestReverseRepoHistorySnapshotCoverageAndScope(t *testing.T) {
	snapshot, err := loadReverseRepoHistory()
	require.NoError(t, err)
	require.Len(t, snapshot.Events, 1606)
	require.Equal(t, "2015-01-22", snapshot.Events[0].PublicationDate)
	require.Equal(t, "2024-07-19", snapshot.Events[len(snapshot.Events)-1].PublicationDate)
	for _, event := range snapshot.Events {
		require.Equal(t, "7天", event.Term)
		require.NotContains(t, event.AwardedRateRaw, "14天")
		require.Equal(t, "中国人民银行公开市场业务操作室", event.Publisher)
		require.Equal(t, "中国货币网", event.Host)
		require.Contains(t, event.SourceURL, "/chinese/scggyhywgggksccz/")
	}
	var mixedMLF ReverseRepoHistoryEvent
	for _, event := range snapshot.Events {
		if event.PublicationDate == "2021-06-15" {
			mixedMLF = event
			break
		}
	}
	require.Equal(t, 2.2, mixedMLF.RatePercent, "必须取同一7天逆回购表格行，不能混入MLF利率")
}

func TestReverseRepoHistoryValidatorRejectsSLO(t *testing.T) {
	snapshot, err := loadReverseRepoHistory()
	require.NoError(t, err)
	snapshot.Events[0].Title = "短期流动性调节工具(SLO)交易公告 [2015]第1号"
	snapshot.Events[0].Term = "12月17日"
	require.Error(t, validateReverseRepoHistorySnapshot(snapshot))
}

func TestParseReverseRepoHistoryNoticeSeparatesSevenAndFourteenDay(t *testing.T) {
	event, err := parseReverseRepoHistoryNotice(reverseRepoHistoryFixture, "https://www.chinamoney.com.cn/chinese/scggyhywgggksccz/20231221/2776242.html", "2023-12-21")
	require.NoError(t, err)
	require.NotNil(t, event)
	require.Equal(t, "2023-12-21", event.OperationDate)
	require.Equal(t, "2023-12-21 09:20", event.PublishedAt)
	require.Equal(t, "7天", event.Term)
	require.Equal(t, "1.80%", event.AwardedRateRaw)
	require.Equal(t, 1.8, event.RatePercent)
}

func TestReverseRepoHistoryRequestRejectsPostMechanismRange(t *testing.T) {
	require.Error(t, (ReverseRepoHistoryRequest{Begin: "20240722", End: "20240722"}).Validate())
	require.Error(t, (ReverseRepoHistoryRequest{Begin: "20240719", End: "20240720"}).Validate())
	require.NoError(t, (ReverseRepoHistoryRequest{Begin: "20240719", End: "20240719"}).Validate())
}

func TestReverseRepoHistoryUsesSnapshotByDefault(t *testing.T) {
	result, err := NewClient("").ReverseRepoHistory(context.Background(), ReverseRepoHistoryRequest{Begin: "20240718", End: "20240719"})
	require.NoError(t, err)
	require.Equal(t, "embedded-history", result.UpdateStatus)
	require.Len(t, result.Events, 2)
	require.Equal(t, result.TotalEvents, len(result.Events))
}

func TestReverseRepoHistoryRefreshDoesNotAppendFixedRateEra(t *testing.T) {
	defer gock.Off()
	gock.New(reverseRepoHistoryOrigin).
		Post("/ags/ms/cm-s-notice-query/contents").
		MatchParam("pageNo", "1").
		Reply(200).
		JSON(map[string]any{
			"data": map[string]any{"pageTotalSize": 1},
			"records": []map[string]any{{
				"title": "公开市场业务交易公告2024年第143号", "releaseDate": "2024-07-22",
				"draftPath": "/chinese/scggyhywgggksccz/20240722/test.html",
			}},
		})
	client := NewClient("")
	client.Refresh = true
	result, err := client.ReverseRepoHistory(context.Background(), ReverseRepoHistoryRequest{Begin: "20240719", End: "20240719"})
	require.NoError(t, err)
	require.Equal(t, "sealed-at-2024-07-19", result.UpdateStatus)
	require.Len(t, result.Events, 1)
	require.True(t, gock.IsDone())
}
