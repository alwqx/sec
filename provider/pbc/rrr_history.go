package pbc

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 历史快照由已核验的官网全量结果生成；保留原始来源、正文及实际抓取时间。
//
//go:embed data/rrr-history.json
var embeddedRRRHistory []byte

type rrrHistory struct {
	VerifiedThrough   string     `json:"verified_through"`
	VerifiedAt        time.Time  `json:"verified_at"`
	AnnouncementCount int        `json:"announcement_count"`
	Events            []RRREvent `json:"events"`
}

type rrrUpdateState struct {
	HistoryVersion string     `json:"history_version"`
	CheckedThrough string     `json:"checked_through"`
	CheckedAt      time.Time  `json:"checked_at"`
	Events         []RRREvent `json:"events"`
}

func rrrBeijingDate(date time.Time) string {
	return date.In(time.FixedZone("CST", 8*60*60)).Format("20060102")
}

func loadRRRHistory() (rrrHistory, error) {
	var history rrrHistory
	if err := json.Unmarshal(embeddedRRRHistory, &history); err != nil {
		return history, fmt.Errorf("读取内置准备金率历史失败: %w", err)
	}
	if _, err := time.Parse("20060102", history.VerifiedThrough); err != nil || history.VerifiedThrough != rrrBeijingDate(history.VerifiedAt) || len(history.Events) == 0 {
		return history, fmt.Errorf("内置准备金率历史检查点无效")
	}
	return history, nil
}

func rrrEventKey(event RRREvent) string {
	return fmt.Sprintf("%s|%s|%s|%g|%g|%t", event.EffectiveDate, event.Scope, event.Exclusions, event.ChangePPLower, event.ChangePPUpper, event.Additional)
}

func mergeRRREvents(groups ...[]RRREvent) []RRREvent {
	result := []RRREvent{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, event := range group {
			key := rrrEventKey(event)
			if !seen[key] {
				seen[key] = true
				result = append(result, event)
			}
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].EffectiveDate < result[j].EffectiveDate })
	return result
}

func (c *Client) loadRRRUpdates(history rrrHistory) rrrUpdateState {
	version := history.VerifiedThrough + "@" + history.VerifiedAt.UTC().Format(time.RFC3339Nano)
	state := rrrUpdateState{HistoryVersion: version, CheckedThrough: history.VerifiedThrough, CheckedAt: history.VerifiedAt, Events: []RRREvent{}}
	if c.CacheDir == "" {
		return state
	}
	raw, err := os.ReadFile(filepath.Join(c.CacheDir, "rrr-updates.json"))
	if err != nil {
		return state
	}
	var saved rrrUpdateState
	if json.Unmarshal(raw, &saved) != nil || saved.HistoryVersion != version || saved.CheckedThrough < history.VerifiedThrough || saved.CheckedThrough != rrrBeijingDate(saved.CheckedAt) || saved.CheckedAt.After(time.Now()) {
		return state // 缓存损坏或历史快照已升级时，从内置快照重新增量发现。
	}
	for _, event := range saved.Events {
		if _, err := time.Parse("20060102", event.EffectiveDate); err != nil || event.EffectiveDate < RRRCoverageStart {
			return state
		}
		if _, err := c.resolve(c.origin, event.Source); err != nil || event.Source == "" || event.Title == "" || event.AnnouncementText == "" || event.FetchedAt.IsZero() || event.Scope == "" || (event.ScopeType != "broad" && event.ScopeType != "targeted") || event.ChangePPLower > event.ChangePPUpper || math.Abs(event.ChangePPLower) > 100 || math.Abs(event.ChangePPUpper) > 100 || (event.ChangePPLower <= 0 && event.ChangePPUpper >= 0) {
			return state
		}
		published := strings.ReplaceAll(strings.Split(event.PublishedAt, "T")[0], "-", "")
		if _, err := time.Parse("20060102", published); err != nil || published < history.VerifiedThrough || published > saved.CheckedThrough {
			return state
		}
	}
	return saved
}

func (c *Client) saveRRRUpdates(state rrrUpdateState) error {
	if c.CacheDir == "" {
		return nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(c.CacheDir, ".rrr-updates-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(raw)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(c.CacheDir, "rrr-updates.json"))
}

// RRR 合并内置历史、本地增量和最新公告，按生效日返回结果。
func (c *Client) RRR(ctx context.Context, req RRRRequest) (RRRResult, error) {
	history, err := loadRRRHistory()
	if err != nil {
		return RRRResult{}, err
	}
	return c.rrrWithHistory(ctx, req, history)
}

func (c *Client) rrrWithHistory(ctx context.Context, req RRRRequest, history rrrHistory) (RRRResult, error) {
	result := RRRResult{Region: "CN", Currency: "CNY", Indicator: "rmb-reserve-requirement-ratio-adjustments", Unit: "百分点", Begin: req.Begin, End: req.End, CoverageStart: RRRCoverageStart, CoverageBasis: "announcement-publication-date", Events: []RRREvent{}, ListSource: c.origin + rrrNewsPath, HistoryVerifiedThrough: history.VerifiedThrough, HistoryEventCount: len(history.Events), UpdateStatus: "history-only", Note: "内置已核验历史加增量公告，按生效日筛选；负值为下调。不同机构范围、豁免条件和额外降幅须分别解读，不计算统一准备金率水平。网页公布时间为当前元数据，非首次发布存档。"}
	if err := req.Validate(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	state := c.loadRRRUpdates(history)
	result.CachedUpdateCount = len(state.Events)
	if req.End >= state.CheckedThrough || c.Refresh {
		known := map[string]bool{}
		for _, group := range [][]RRREvent{history.Events, state.Events} {
			for _, event := range group {
				known[event.Source] = true
			}
		}
		updates, err := c.scanRRR(ctx, state.CheckedThrough, known)
		if err != nil {
			return result, err // 需要核验最新数据时，不把网络失败伪装为没有新增公告。
		}
		result.ListFetchedAt, result.ScannedPages, result.ScannedAnnouncements = updates.ListFetchedAt, updates.ScannedPages, updates.ScannedAnnouncements
		result.UpdateStatus = "checked-latest"
		state.Events = mergeRRREvents(state.Events, updates.Events)
		checked := rrrBeijingDate(*updates.ListFetchedAt)
		if checked >= state.CheckedThrough {
			state.CheckedThrough, state.CheckedAt = checked, *updates.ListFetchedAt
		}
		if err := c.saveRRRUpdates(state); err != nil {
			return result, fmt.Errorf("保存准备金率增量失败: %w", err)
		}
	}
	result.UpdatesCheckedThrough = state.CheckedThrough
	for _, event := range mergeRRREvents(history.Events, state.Events) {
		if event.EffectiveDate >= req.Begin && event.EffectiveDate <= req.End {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}
