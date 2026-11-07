//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alwqx/sec/financials"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/usstock"
)

// edgarClient 用共享配置里的联系邮箱访问 SEC。
func (s *desktopServer) edgarClient(ctx context.Context) (*edgar.Client, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &edgar.Client{Email: cfg.SECEmail}, nil
}

// handleUSCompany 返回美股公司资料：SEC EDGAR 公司信息和新浪行情，任一失败时照常返回另一部分并说明原因。
func (s *desktopServer) handleUSCompany(w http.ResponseWriter, r *http.Request) {
	s.serveUS(w, r, "公司资料", func(ctx context.Context, client *edgar.Client, code string) (any, error) {
		info, profileErr, quoteErr := usstock.Load(ctx, client, code)
		if profileErr != nil && quoteErr != nil {
			return nil, usUpstream{"美股公司资料和行情都获取失败，请稍后重试", fmt.Errorf("公司资料：%w；行情：%w", profileErr, quoteErr)}
		}
		notes := []string{}
		switch {
		case errors.Is(profileErr, edgar.ErrNotFound):
			notes = append(notes, "SEC EDGAR 没有这个代码，基金、场外交易股票等可能不向 SEC 申报公司资料。")
		case profileErr != nil:
			notes = append(notes, "公司资料获取失败："+profileErr.Error())
		}
		if quoteErr != nil {
			notes = append(notes, "行情获取失败："+quoteErr.Error())
		}
		return map[string]any{"info": info, "notes": notes}, nil
	})
}

// usFactsCache 缓存 XBRL 数据（见 edgar.FactsCache），财报、分红和估值栏目共用。
var usFactsCache = &edgar.FactsCache{}

// cachedUSFacts 返回先查缓存、过期或没有时再下载的 XBRL 读取函数。
func cachedUSFacts(client *edgar.Client) func(context.Context, int) (*edgar.Facts, error) {
	return func(ctx context.Context, cik int) (*edgar.Facts, error) { return usFactsCache.Facts(ctx, client, cik) }
}

// usBadRequest 是美股接口的参数错误，返回 400。
type usBadRequest string

func (e usBadRequest) Error() string { return string(e) }

// usUpstream 是要用自定义提示返回的上游失败（502），比如公司资料和行情都失败。err 只写进日志，不提供 Unwrap：
// 内层可能有 edgar.ErrNotFound，不能让 usFail 按 404 处理。
type usUpstream struct {
	message string
	err     error
}

func (e usUpstream) Error() string {
	if e.err == nil {
		return e.message
	}
	return e.message + "：" + e.err.Error()
}

// serveUS 是美股 SEC 接口共用的准备：先校验美股代码（无效时 400），再创建带 30 秒超时的 ctx 和用共享配置联系邮箱的 SEC 客户端
// （读取失败时 500），交给 handle；handle 的结果写成 JSON，错误按 usFail 映射。cancel 在这里 defer，handle 不用管。
// what 是数据的名称，比如"财报"，用在错误提示里。
func (s *desktopServer) serveUS(w http.ResponseWriter, r *http.Request, what string, handle func(ctx context.Context, client *edgar.Client, code string) (any, error)) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if !usTicker.MatchString(code) {
		writeError(w, http.StatusBadRequest, "只支持美股代码，比如 $AAPL")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client, err := s.edgarClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "读取共享配置失败", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "读取 SEC 联系邮箱失败，请在设置中检查")
		return
	}
	result, err := handle(ctx, client, code)
	if err != nil {
		usFail(w, r, err, what, code)
		return
	}
	writeJSON(w, result)
}

// usFacts 查到公司并读取 XBRL 数据，和财报、估值、分红栏目共用缓存。
func usFacts(ctx context.Context, client *edgar.Client, code string) (edgar.Company, *edgar.Facts, error) {
	company, err := client.Lookup(ctx, code)
	if err != nil {
		return company, nil, err
	}
	facts, err := cachedUSFacts(client)(ctx, company.CIK)
	return company, facts, err
}

// usFail 写出错误响应：参数错误 400；请求已取消时不写；usUpstream 用它的提示返回 502；SEC 没有这家公司或没有 XBRL 数据时 404；
// 数据不足以整理（没有利润表、IFRS 公司没有季度数据）时 404 并说明原因；其他是上游失败（502）。日志带上请求路径，区分是哪个接口。
func usFail(w http.ResponseWriter, r *http.Request, err error, what, code string) {
	var bad usBadRequest
	var upstream usUpstream
	switch {
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, bad.Error())
	case r.Context().Err() != nil:
	case errors.As(err, &upstream):
		slog.ErrorContext(r.Context(), "查询美股"+what+"失败", "path", r.URL.Path, "code", code, "error", upstream.err)
		writeError(w, http.StatusBadGateway, upstream.message)
	case errors.Is(err, edgar.ErrNotFound) || errors.Is(err, edgar.ErrNoXBRL):
		writeError(w, http.StatusNotFound, "SEC EDGAR 没有这家公司的"+what+"数据，可能不向 SEC 申报")
	case errors.Is(err, financials.ErrNoSECData) || errors.Is(err, financials.ErrIFRSQuarterly):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		slog.ErrorContext(r.Context(), "查询美股"+what+"失败", "path", r.URL.Path, "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "美股"+what+"获取失败："+err.Error())
	}
}

// usQuarterly 读取年度或季度参数 name：空和 annual 是年度，quarter 是季度，其他值是参数错误。
func usQuarterly(r *http.Request, name string) (bool, error) {
	switch r.URL.Query().Get(name) {
	case "", "annual":
		return false, nil
	case "quarter":
		return true, nil
	}
	return false, usBadRequest(name + " 须为 annual 或 quarter")
}

// handleUSFinancials 返回美股财报的一张报表：年度最多 10 期，季度最多 12 期，从新到旧。
func (s *desktopServer) handleUSFinancials(w http.ResponseWriter, r *http.Request) {
	s.serveUS(w, r, "财报", func(ctx context.Context, client *edgar.Client, code string) (any, error) {
		statement := edgar.Statement(r.URL.Query().Get("statement"))
		if _, ok := edgar.Lines[statement]; !ok {
			return nil, usBadRequest("报表须为 income、balance 或 cashflow")
		}
		quarterly, err := usQuarterly(r, "period")
		if err != nil {
			return nil, err
		}
		company, facts, err := usFacts(ctx, client, code)
		if err != nil {
			return nil, err
		}
		limit := 10
		if quarterly {
			limit = 12
		}
		return map[string]any{"ticker": company.Ticker, "name": facts.Name, "cik": company.CIK, "report": facts.Build(statement, quarterly, limit)}, nil
	})
}

// handleUSDividends 返回美股按财年的分红表（usstock.Dividends），和 CLI 的 sec info -d 相同。
func (s *desktopServer) handleUSDividends(w http.ResponseWriter, r *http.Request) {
	s.serveUS(w, r, "分红", func(ctx context.Context, client *edgar.Client, code string) (any, error) {
		return usstock.LoadDividends(ctx, client, code, nil, cachedUSFacts(client))
	})
}

// handleUSFinancialCharts 返回美股财报的图表数据（financials.FromSEC），格式和 A 股的 /api/v1/financials/charts 相同。
func (s *desktopServer) handleUSFinancialCharts(w http.ResponseWriter, r *http.Request) {
	s.serveUS(w, r, "财报", func(ctx context.Context, client *edgar.Client, code string) (any, error) {
		quarterly, err := usQuarterly(r, "freq")
		if err != nil {
			return nil, err
		}
		company, facts, err := usFacts(ctx, client, code)
		if err != nil {
			return nil, err
		}
		return financials.FromSEC(company.Ticker, facts, quarterly)
	})
}
