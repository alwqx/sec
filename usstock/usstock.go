// Package usstock 汇总美股公司资料：公司信息来自 SEC EDGAR，行情和估值来自新浪。CLI 和 Desktop 共用。
package usstock

import (
	"context"
	"sync"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
)

// Info 是一家美股公司的资料。Company 在 SEC 没有这个代码（比如部分基金、场外交易股票）时为 nil，Quote 在行情获取失败时为 nil。
type Info struct {
	ExCode  string         `json:"ex_code"`
	Ticker  string         `json:"ticker"`
	Company *edgar.Profile `json:"company"`
	Quote   *sina.USQuote  `json:"quote"`
}

// Load 并发查询公司资料和行情，返回两边各自的错误；调用方决定只有一边失败时怎么处理。
// 新浪行情用原代码查询，类别股的写法和 SEC 不同（新浪 brk$b，SEC BRK-B）。
func Load(ctx context.Context, client *edgar.Client, exCode string) (info Info, profileErr, quoteErr error) {
	info = Info{ExCode: exCode, Ticker: edgar.NormalizeTicker(exCode)}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		company, err := client.Lookup(ctx, exCode)
		if err == nil {
			info.Company, err = client.Profile(ctx, company.CIK)
		}
		profileErr = err
	}()
	go func() {
		defer wg.Done()
		info.Quote, quoteErr = sina.QueryUSQuote(ctx, exCode)
	}()
	wg.Wait()
	return info, profileErr, quoteErr
}
