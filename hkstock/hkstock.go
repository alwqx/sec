// Package hkstock 组合新浪和东方财富的数据，生成港股公司概况，CLI info、kline 和 Desktop 共用。
package hkstock

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
)

// ErrNoData 表示东方财富没有这只证券的数据，比如刚上市或东方财富未覆盖，重试也没有用。
var ErrNoData = errors.New("东方财富没有数据")

// Result 是港股公司概况及东方财富各部分的错误。Profile 总有值；某项错误非 nil 时对应的字段留空，
// 没有数据时错误是 ErrNoData。不是股本证券（新浪类型不是 EQTY）时没有公司资料和估值，这些错误都是 nil。
type Result struct {
	Profile       *sina.CorpProfile
	ProfileErr    error // 东方财富公司资料
	IssuePriceErr error // 东方财富发行价
	ValuationErr  error // 东方财富市盈率、市净率
}

// Equity 报告是否股本证券。交易所买卖基金、房地产投资信托（TRST）、权证（WRNT）等不是公司，
// F10 里的公司名称是基金管理人或没有资料，只输出行情部分。
func Equity(profile *sina.CorpProfile) bool {
	return profile.HKType == "EQTY"
}

// Load 查询港股公司概况，exCode 如 HK00700。
// 名称、价格和市值来自新浪行情；公司资料来自东方财富港股 F10，市盈率和市净率来自东方财富行情。
// 新浪行情失败时报错，代码不存在时是 sina.ErrNotFound。东方财富的公司资料和估值分别请求，
// 一项失败或没有数据只留空这一项，不影响其他内容。
// 三项同时请求以免股票多等一轮；不是股本证券时东方财富的结果不用。
func Load(ctx context.Context, exCode string) (*Result, error) {
	code := strings.ToUpper(exCode)[2:]
	var (
		wg                      sync.WaitGroup
		profile                 *sina.CorpProfile
		hk                      *eastmoney.HKProfile
		pe, pb                  *float64
		quoteErr, hkErr, valErr error
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		profile, quoteErr = sina.Profile(ctx, &types.InfoOptions{Code: code, ExCode: exCode})
	}()
	go func() {
		defer wg.Done()
		hk, hkErr = eastmoney.GetHKProfile(ctx, code)
	}()
	go func() {
		defer wg.Done()
		pe, pb, valErr = eastmoney.GetHKValuation(ctx, code)
	}()
	wg.Wait()
	if quoteErr != nil {
		return nil, quoteErr
	}
	res := &Result{Profile: profile}
	if !Equity(profile) {
		return res, nil
	}

	switch {
	case hkErr != nil:
		res.ProfileErr = hkErr
	case hk == nil:
		res.ProfileErr = ErrNoData
	default:
		merge(profile, hk)
		res.IssuePriceErr = hk.IssuePriceErr
	}
	switch {
	case valErr != nil:
		res.ValuationErr = valErr
	case pe == nil && pb == nil:
		res.ValuationErr = ErrNoData
	}
	if pe != nil {
		profile.PeTTM = *pe
	}
	if pb != nil {
		profile.PB = *pb
	}
	return res, nil
}

// Profile 和 Load 相同，东方财富的失败只记 WARN，CLI info、kline 用。
func Profile(ctx context.Context, exCode string) (*sina.CorpProfile, error) {
	res, err := Load(ctx, exCode)
	if err != nil {
		return nil, err
	}
	if res.ProfileErr != nil {
		slog.WarnContext(ctx, "港股公司资料获取失败，只显示行情", "code", exCode, "error", res.ProfileErr)
	}
	if res.IssuePriceErr != nil {
		slog.WarnContext(ctx, "港股发行价获取失败", "code", exCode, "error", res.IssuePriceErr)
	}
	if res.ValuationErr != nil {
		slog.WarnContext(ctx, "港股估值获取失败，市盈率和市净率留空", "code", exCode, "error", res.ValuationErr)
	}
	return res.Profile, nil
}

func merge(profile *sina.CorpProfile, hk *eastmoney.HKProfile) {
	if hk.Name != "" {
		profile.Name = hk.Name
	}
	if hk.ShortName != "" {
		profile.HistoryName = hk.ShortName
	}
	profile.ListingDate = hk.ListingDate
	if hk.IssuePrice != nil {
		profile.ListingPrice = *hk.IssuePrice
	}
	profile.Category = hk.Industry
	profile.WebSite = hk.Website
	profile.BusinessAddress = hk.Address
	profile.RegisterAddress = hk.RegisterAddress
	profile.MainBusiness = hk.MainBusiness
	profile.Description = hk.Description
}
