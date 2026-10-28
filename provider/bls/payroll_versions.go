package bls

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/gofrs/flock"
)

// PayrollVersion 是本机实际观测到的岗位水平或脚注版本；时间不是官方发布日期。
type PayrollVersion struct {
	Employment float64    `json:"employment_thousands"`
	RawValue   string     `json:"raw_value"`
	Footnotes  []Footnote `json:"footnotes"`
	ObservedAt time.Time  `json:"observed_at"`
}

type payrollHistory struct {
	Schema   int                         `json:"schema"`
	SeriesID string                      `json:"series_id"`
	Periods  map[string][]PayrollVersion `json:"periods"`
}

func (c *Client) observePayrollVersions(ctx context.Context, values map[string]*monthlyValue) (map[string][]PayrollVersion, error) {
	if c.CacheDir == "" {
		return map[string][]PayrollVersion{}, nil
	}
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(c.CacheDir, "payrolls-observed.json")
	lock := flock.New(path + ".lock")
	ok, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !ok {
		_ = lock.Close()
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("等待非农观测档案锁失败: %w", err)
		}
		return nil, fmt.Errorf("未取得非农观测档案锁")
	}
	defer func() { _ = lock.Unlock(); _ = lock.Close() }()
	history := payrollHistory{Schema: 1, SeriesID: payrollSeriesID, Periods: map[string][]PayrollVersion{}}
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &history); err != nil {
			return nil, fmt.Errorf("非农本机版本档案损坏，未覆盖原文件: %w", err)
		}
		if err := validatePayrollHistory(&history); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	changed := false
	for period, point := range values {
		if point.Number == nil {
			continue
		}
		versions := history.Periods[period]
		if len(versions) > 0 {
			last := versions[len(versions)-1]
			if !point.FetchedAt.After(last.ObservedAt) {
				continue
			}
			if *point.Number == last.Employment && reflect.DeepEqual(point.Footnotes, last.Footnotes) {
				continue
			}
		}
		history.Periods[period] = append(versions, PayrollVersion{*point.Number, point.RawValue, point.Footnotes, point.FetchedAt})
		changed = true
	}
	if changed {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(history)
		if err != nil {
			return nil, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".payrolls-history-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(raw)
		closeErr := f.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.Rename(f.Name(), path); err != nil {
			return nil, err
		}
	}
	return history.Periods, nil
}

func validatePayrollHistory(history *payrollHistory) error {
	invalid := func() error { return fmt.Errorf("非农本机版本档案格式无效，未覆盖原文件") }
	if history.Schema != 1 || history.SeriesID != payrollSeriesID || history.Periods == nil {
		return invalid()
	}
	for period, versions := range history.Periods {
		if _, err := time.Parse("200601", period); err != nil || len(period) != 6 || len(versions) == 0 {
			return invalid()
		}
		var previous time.Time
		for _, version := range versions {
			if math.IsNaN(version.Employment) || math.IsInf(version.Employment, 0) || version.Employment < 0 || version.ObservedAt.IsZero() || !version.ObservedAt.After(previous) {
				return invalid()
			}
			previous = version.ObservedAt
		}
	}
	return nil
}
