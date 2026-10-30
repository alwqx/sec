package mofcom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const servicesURL = "https://data.mofcom.gov.cn/datamofcom/front/fwmy/mouthly"
const servicesPage = "https://data.mofcom.gov.cn/fwmy/monthly.shtml"

// ServicesRequest 查询商务部独立发布的服务贸易月值，不从累计值差分。
type ServicesRequest struct{ Begin, End string }

func (r *ServicesRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("服务贸易查询请求不能为空")
	}
	if err := (&TradeRequest{Begin: r.Begin, End: r.End}).Validate(); err != nil {
		return err
	}
	if r.Begin < "201601" {
		return fmt.Errorf("服务贸易月度查询支持自201601起，历史缺口不补值")
	}
	return nil
}

type ServicesValues struct {
	Currency string   `json:"currency"`
	Unit     string   `json:"unit"`
	Total    *float64 `json:"total"`
	Exports  *float64 `json:"exports"`
	Imports  *float64 `json:"imports"`
	Balance  *float64 `json:"balance"`
}

type ServicesObservation struct {
	Period      string                     `json:"period"`
	CNY         *ServicesValues            `json:"cny"`
	USD         *ServicesValues            `json:"usd"`
	Source      string                     `json:"source,omitempty"`
	FetchedAt   *time.Time                 `json:"fetched_at"`
	PublishedAt *time.Time                 `json:"published_at"`
	RevisionID  *string                    `json:"revision_id"`
	RawValues   map[string]json.RawMessage `json:"raw_values,omitempty"`
	Note        string                     `json:"note,omitempty"`
}

type ServicesResult struct {
	Region             string                `json:"region"`
	Indicator          string                `json:"indicator"`
	Frequency          string                `json:"frequency"`
	PeriodBasis        string                `json:"period_basis"`
	PriceBasis         string                `json:"price_basis"`
	SeasonallyAdjusted bool                  `json:"seasonally_adjusted"`
	LevelAnnualized    bool                  `json:"level_annualized"`
	Begin              string                `json:"begin"`
	End                string                `json:"end"`
	FirstPeriod        string                `json:"first_period"`
	LastPeriod         string                `json:"last_period"`
	Source             string                `json:"source"`
	DataVersion        string                `json:"data_version"`
	Observations       []ServicesObservation `json:"observations"`
	Note               string                `json:"note"`
}

type servicesEnvelope struct {
	Rows     []map[string]json.RawMessage `json:"rows"`
	Total    int                          `json:"total"`
	PageSize int                          `json:"pageSize"`
	Page     int                          `json:"pageNumber"`
	Pages    int                          `json:"maxPageNum"`
}

func parseServices(raw []byte, begin, end string, page int) (servicesEnvelope, []ServicesObservation, error) {
	env := servicesEnvelope{Total: -1}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, nil, fmt.Errorf("服务贸易响应无效：%w", err)
	}
	if env.Total < 0 || env.PageSize <= 0 || env.PageSize > 1000 || env.Page != page || env.Rows == nil || env.Pages != max(1, (env.Total+env.PageSize-1)/env.PageSize) || page > env.Pages || env.Pages > 300 {
		return env, nil, fmt.Errorf("服务贸易分页元数据无效")
	}
	expected := min(env.PageSize, env.Total-(page-1)*env.PageSize)
	if len(env.Rows) != expected {
		return env, nil, fmt.Errorf("服务贸易分页记录数量与总数不符")
	}
	items := []ServicesObservation{}
	previous := "999999"
	for _, row := range env.Rows {
		var period string
		if err := json.Unmarshal(row["mouth"], &period); err != nil {
			return env, nil, fmt.Errorf("服务贸易缺少所属月份")
		}
		date, err := time.Parse("200601", period)
		if err != nil || date.Format("200601") != period || period < begin || period > end || period >= previous {
			return env, nil, fmt.Errorf("服务贸易月份无效、重复、越界或排序变化：%s", period)
		}
		previous = period
		item := ServicesObservation{Period: period, RawValues: row}
		for _, spec := range []struct {
			suffix, currency, unit string
			dest                   **ServicesValues
		}{{"RMB", "CNY", "亿元人民币", &item.CNY}, {"USD", "USD", "亿美元", &item.USD}} {
			values := &ServicesValues{Currency: spec.currency, Unit: spec.unit}
			for _, field := range []struct {
				key  string
				dest **float64
			}{{"importAndExport", &values.Total}, {"export", &values.Exports}, {"import", &values.Imports}, {"difference", &values.Balance}} {
				text, ok := row[field.key+spec.suffix]
				if !ok {
					return env, nil, fmt.Errorf("服务贸易缺少%s字段", field.key+spec.suffix)
				}
				value, err := parseNumber(text)
				if err != nil || value != nil && field.key != "difference" && *value < 0 {
					return env, nil, fmt.Errorf("服务贸易%s数值无效", field.key+spec.suffix)
				}
				*field.dest = value
			}
			*spec.dest = values
		}
		items = append(items, item)
	}
	return env, items, nil
}

// Services 逐页校验月值；人民币和美元均读取原值，不重算官方差额。
func (c *Client) Services(ctx context.Context, req *ServicesRequest) (*ServicesResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	rows := map[string]ServicesObservation{}
	previous := "999999"
	var total, size, pages int
	for page := 1; ; page++ {
		address := servicesURL + "?" + url.Values{"start_date": {req.Begin}, "end_date": {req.End}, "pageNumber": {strconv.Itoa(page)}}.Encode()
		var env servicesEnvelope
		var items []ServicesObservation
		_, fetched, err := c.request(ctx, http.MethodGet, address, servicesPage, nil, nil, func(raw []byte) error {
			var err error
			env, items, err = parseServices(raw, req.Begin, req.End, page)
			if err != nil {
				return err
			}
			if page > 1 && (env.Total != total || env.PageSize != size || env.Pages != pages) {
				return fmt.Errorf("服务贸易分页期间总数或页面大小发生变化")
			}
			if len(items) > 0 && items[0].Period >= previous {
				return fmt.Errorf("服务贸易跨页月份重复或排序变化")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		total, size, pages = env.Total, env.PageSize, env.Pages
		for _, item := range items {
			item.Source, item.FetchedAt = address, &fetched
			rows[item.Period] = item
			previous = item.Period
		}
		if page == pages {
			break
		}
	}
	result := &ServicesResult{Region: "CN", Indicator: "services_trade", Frequency: "monthly", PeriodBasis: "monthly", PriceBasis: "nominal", Begin: req.Begin, End: req.End, Source: servicesPage, DataVersion: "current-visible-revision", Observations: []ServicesObservation{}, Note: "商务部服务进出口月度统计；现价、未季调、不折年，与海关货物贸易分开。人民币与美元各自读取上游值，不按汇率换算；总额和差额读取官方字段，不用出口加减进口覆盖独立舍入。未列示的月份（包括可能缺失的1—2月）保留空值，不从累计值差分。历史为当前可见版本；源创建/更新时间不等同于逐期公布时间，published_at/revision_id为null。伙伴分项未提供。"}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for date := begin; !date.After(end); date = date.AddDate(0, 1, 0) {
		period := date.Format("200601")
		item, exists := rows[period]
		if !exists {
			item = ServicesObservation{Period: period, Note: "上游未列示该月服务贸易记录"}
		} else {
			if result.FirstPeriod == "" {
				result.FirstPeriod = period
			}
			result.LastPeriod = period
		}
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}
