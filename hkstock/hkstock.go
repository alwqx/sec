// Package hkstock 组合新浪和东方财富的数据，生成港股公司概况，CLI info、kline 共用。
package hkstock

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
)

// Profile 查询港股公司概况，exCode 如 HK00700。
// 名称、价格和市值来自新浪行情；公司资料来自东方财富港股 F10，市盈率和市净率来自东方财富行情。
// 新浪行情失败时报错。东方财富的公司资料和估值分别请求，一项失败或没有数据只留空这一项、记 WARN，不影响其他内容。
// 交易所买卖基金、房地产投资信托不是公司，F10 里的公司名称是基金管理人，只输出行情部分。
func Profile(ctx context.Context, exCode string) (*sina.CorpProfile, error) {
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
	if profile.HKTrust {
		return profile, nil
	}

	if hkErr != nil {
		slog.WarnContext(ctx, "港股公司资料获取失败，只显示行情", "code", exCode, "error", hkErr)
	} else if hk != nil {
		merge(profile, hk)
	}
	if valErr != nil {
		slog.WarnContext(ctx, "港股估值获取失败，市盈率和市净率留空", "code", exCode, "error", valErr)
	}
	if pe != nil {
		profile.PeTTM = *pe
	}
	if pb != nil {
		profile.PB = *pb
	}
	return profile, nil
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
