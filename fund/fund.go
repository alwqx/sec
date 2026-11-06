// Package fund 组合天天基金 App 接口和网页的数据，生成基金概况，CLI info 和 Desktop 共用。
package fund

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alwqx/sec/provider/eastmoney"
)

// ErrNotFound 表示两个数据源都没有这只基金。
var ErrNotFound = errors.New("没有找到这只基金")

// Info 是基金概况，也是 sec info 基金的 --json 输出。上游没有的值是空字符串或 null。
type Info struct {
	ExCode        string   `json:"ex_code"`
	Code          string   `json:"code"`
	Name          string   `json:"name"`      // 基金简称
	FullName      string   `json:"full_name"` // 基金全称
	Type          string   `json:"type"`      // 基金类型，如 混合型-偏股
	MoneyMarket   bool     `json:"money_market"`
	Listed        bool     `json:"listed"`     // 场内交易（ETF、LOF 等）
	RiskLevel     string   `json:"risk_level"` // 低、中低、中、中高、高
	Company       string   `json:"company"`    // 基金管理人
	Custodian     string   `json:"custodian"`  // 基金托管人
	Managers      string   `json:"managers"`   // 基金经理
	LaunchDate    string   `json:"launch_date"`
	LaunchSize    string   `json:"launch_size"`
	NetAssets     *float64 `json:"net_assets"`      // 净资产（元）
	NetAssetsNote string   `json:"net_assets_note"` // 网页上的净资产规模原文，带截止日期
	ManagementFee string   `json:"management_fee"`
	CustodyFee    string   `json:"custody_fee"`
	Benchmark     string   `json:"benchmark"` // 业绩比较基准
	Tracking      string   `json:"tracking"`  // 跟踪标的
	Objective     string   `json:"objective"` // 投资目标
	// 非货币基金的单位净值、累计净值和日涨幅（%）；货币基金这三项为 null，改用每万份收益（元）和七日年化收益率（%）。
	NAV       *float64               `json:"nav"`
	AccNAV    *float64               `json:"acc_nav"`
	DayChange *float64               `json:"day_change"`
	Yield10K  *float64               `json:"yield_10k"`
	Yield7D   *float64               `json:"yield_7d"`
	NAVDate   string                 `json:"nav_date"`
	Returns   *eastmoney.FundReturns `json:"returns"` // 阶段收益率（%），App 接口失败或一个阶段都没有时为 null
	Notes     []string               `json:"notes"`   // 一个数据源失败时的说明
}

// Load 查询基金概况，exCode 是 OF 加 6 位代码。App 接口提供净值、收益率、风险等级等，网页档案提供全称、托管人、
// 业绩比较基准和投资目标等，两个同时请求。一个失败时照常返回另一个的内容并在 Notes 说明；
// 两个都没有拿到这只基金时：都确认没有是 ErrNotFound，有一个请求失败时返回那个错误。
func Load(ctx context.Context, exCode string) (*Info, error) {
	exCode = strings.ToUpper(exCode)
	code := strings.TrimPrefix(exCode, "OF")
	var (
		wg                   sync.WaitGroup
		basic                *eastmoney.FundBasic
		archive              *eastmoney.FundArchive
		basicErr, archiveErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		basic, basicErr = eastmoney.GetFundBasic(ctx, code)
	}()
	go func() {
		defer wg.Done()
		archive, archiveErr = eastmoney.GetFundArchive(ctx, code)
	}()
	wg.Wait()
	// 两个数据源都没有拿到这只基金：都确认没有时是 ErrNotFound；有一个失败时分不清是否存在，返回请求错误。
	if basic == nil && archive == nil {
		switch {
		case basicErr != nil && archiveErr != nil:
			return nil, fmt.Errorf("基金信息：%w；基金档案：%w", basicErr, archiveErr)
		case basicErr != nil:
			return nil, fmt.Errorf("基金信息：%w", basicErr)
		case archiveErr != nil:
			return nil, fmt.Errorf("基金档案：%w", archiveErr)
		}
		return nil, fmt.Errorf("%w: %s", ErrNotFound, exCode)
	}

	info := &Info{ExCode: exCode, Code: code, Notes: []string{}}
	if basicErr != nil {
		info.Notes = append(info.Notes, "净值、收益率和风险等级获取失败，请稍后重试。")
	}
	if archiveErr != nil {
		info.Notes = append(info.Notes, "基金档案（全称、托管人、业绩比较基准等）获取失败，请稍后重试。")
	}
	if archive != nil {
		info.FullName, info.Type, info.Custodian = archive.FullName, archive.Type, archive.Custodian
		info.Company, info.Managers = archive.Company, archive.Managers
		info.LaunchDate, info.LaunchSize, info.NetAssetsNote = archive.LaunchDate, archive.LaunchSize, archive.NetAssets
		info.ManagementFee, info.CustodyFee = archive.ManagementFee, archive.CustodyFee
		info.Benchmark, info.Tracking, info.Objective = archive.Benchmark, archive.Tracking, archive.Objective
		info.Name = archive.ShortName
	}
	if basic != nil {
		info.RiskLevel, info.Listed, info.NAVDate = basic.RiskLevel, basic.Listed, basic.NAVDate
		info.NetAssets = basic.NetAssets
		if basic.Name != "" {
			info.Name = basic.Name
		}
		if hasReturns(basic.Returns) {
			info.Returns = &basic.Returns
		}
		if basic.Type != "" {
			info.Type = basic.Type
		}
		if info.Company == "" {
			info.Company = basic.Company
		}
		if info.Managers == "" {
			info.Managers = basic.Managers
		}
	}
	info.MoneyMarket = strings.HasPrefix(info.Type, "货币型")
	if basic != nil {
		// 货币基金的 App 接口把每万份收益和七日年化收益率放在单位净值、累计净值两列。
		if info.MoneyMarket {
			info.Yield10K, info.Yield7D = basic.NAV, basic.AccNAV
		} else {
			info.NAV, info.AccNAV, info.DayChange = basic.NAV, basic.AccNAV, basic.DayChange
		}
	}
	if info.Name == "" {
		info.Name = info.FullName
	}
	return info, nil
}

// hasReturns 报告是否至少有一个阶段的收益率。新成立的基金全是「--」。
func hasReturns(r eastmoney.FundReturns) bool {
	for _, value := range []*float64{r.Week, r.Month, r.Quarter, r.HalfYear, r.Year, r.TwoYears, r.ThreeYears, r.YearToDate, r.SinceLaunch} {
		if value != nil {
			return true
		}
	}
	return false
}
