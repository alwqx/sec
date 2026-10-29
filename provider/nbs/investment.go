package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// InvestmentRequest 查询官方月报中的累计投资，月份格式为 YYYYMM。
// real-estate 特指房地产开发投资，不是按行业分的房地产业固定资产投资。
type InvestmentRequest struct {
	Begin  string
	End    string
	Sector string
}

func (r *InvestmentRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("固定资产投资查询参数不能为空")
	}
	if err := validateMonths(r.Begin, r.End); err != nil {
		return err
	}
	start := "202509"
	switch r.Sector {
	case "total", "manufacturing", "infrastructure":
	case "real-estate":
		start = "202510"
	default:
		return fmt.Errorf("投资分项必须是 total、manufacturing、infrastructure 或 real-estate")
	}
	if r.Begin < start {
		return fmt.Errorf("%s 官方发布稿目录已核验覆盖起点为 %s，较早月份档案不完整", r.Sector, start)
	}
	return nil
}

// InvestmentObservation 保留每一期发布稿的统计范围，不推算单月值或未公布金额。
type InvestmentObservation struct {
	Region              string     `json:"region"`
	Sector              string     `json:"sector"`
	Period              string     `json:"period"`
	YTDPeriod           string     `json:"ytd_period"`
	YTDAmount           *float64   `json:"ytd_amount"`
	YTDGrowth           *float64   `json:"ytd_growth_percent"`
	AmountUnit          string     `json:"amount_unit"`
	GrowthUnit          string     `json:"growth_unit"`
	PriceBasis          string     `json:"price_basis"`
	SeasonallyAdjusted  bool       `json:"seasonally_adjusted"`
	Scope               string     `json:"scope"`
	InfrastructureScope string     `json:"infrastructure_scope,omitempty"`
	BasisNote           string     `json:"basis_note"`
	Status              string     `json:"status"`
	Source              string     `json:"source"`
	PublishedAt         *time.Time `json:"published_at"`
	FetchedAt           *time.Time `json:"fetched_at"`
	HistoricalVersion   *string    `json:"historical_version"`
	Note                string     `json:"note"`
}

// InvestmentResult 的覆盖起点是当前目录已核验范围，不宣称更早官方数据不存在。
type InvestmentResult struct {
	Sector       string                  `json:"sector"`
	ArchiveStart string                  `json:"archive_start"`
	Observations []InvestmentObservation `json:"observations"`
}

// Investment 复用发布库目录和六小时缓存，只下载请求月份的相应投资或房地产月报。
func (c *Client) Investment(ctx context.Context, req *InvestmentRequest) (*InvestmentResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &InvestmentResult{Sector: req.Sector, ArchiveStart: "202509", Observations: []InvestmentObservation{}}
	if req.Sector == "real-estate" {
		result.ArchiveStart = "202510"
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	needed := map[string]bool{}
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		if month.Month() != time.January {
			needed[month.Format("200601")] = true
		}
	}
	releases := map[string]industrialRelease{}
	latest := ""
	if len(needed) > 0 {
		var err error
		releases, latest, err = c.investmentReleases(ctx, req, needed)
		if err != nil {
			return nil, err
		}
	}
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		period := month.Format("200601")
		item := InvestmentObservation{Region: "CN", Sector: req.Sector, Period: period, YTDPeriod: period[:4] + "01-" + period, AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", Status: "not_published", Note: "尚未检索到该月官方发布稿；不以缺失值表示零"}
		if month.Month() == time.January {
			item.Status = "january_exempt"
			item.Note = "1月份免报；1—2月合并累计于2月所属期发布，不拆成单月值"
		} else if release, ok := releases[period]; ok {
			parsed, fetched, err := c.investmentRelease(ctx, release, req.Sector, period)
			if err != nil {
				return nil, err
			}
			item = *parsed
			item.Source = release.URL
			item.FetchedAt = &fetched
		} else if period <= latest {
			return nil, fmt.Errorf("官方发布稿目录存在档案缺口：%s %s；未用其他口径补值", req.Sector, period)
		}
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

type investmentReleaseFeed struct {
	Success bool                `json:"success"`
	Data    []industrialRelease `json:"data"`
}

func (c *Client) investmentReleases(ctx context.Context, req *InvestmentRequest, needed map[string]bool) (map[string]industrialRelease, string, error) {
	found := map[string]industrialRelease{}
	seenPages := map[string]bool{}
	latest := ""
	for page := 1; page <= 100; page++ {
		var feed investmentReleaseFeed
		validate := func(raw []byte) error {
			return validateInvestmentFeed(raw, page, req.Sector, needed, found, seenPages, latest)
		}
		_, err := c.requestValidated(ctx, "/new/queryCMSArticles", url.Values{"code": {"zxfb"}, "pagenum": {strconv.Itoa(page)}, "pageSize": {"100"}}, nil, &feed, validate)
		if err != nil {
			return nil, "", fmt.Errorf("查询投资发布稿目录失败: %w", err)
		}
		// 回调可能先验证坏缓存再验证网络响应；仅在请求成功后推进分页状态。
		for _, release := range feed.Data {
			period := investmentReleasePeriod(release.Title, req.Sector)
			if period == "" {
				continue
			}
			if period > latest {
				latest = period
			}
			if needed[period] {
				found[period] = release
			}
		}
		seenPages[investmentFeedSignature(feed.Data)] = true
		if len(found) == len(needed) || len(feed.Data) < 100 {
			return found, latest, nil
		}
	}
	return nil, "", fmt.Errorf("投资发布稿目录超过 100 页，请缩小查询范围或核验官方目录")
}

// 校验原始缓存或网络响应，不改变 found/seenPages/latest 查询状态。
func validateInvestmentFeed(raw []byte, page int, sector string, needed map[string]bool, found map[string]industrialRelease, seenPages map[string]bool, latest string) error {
	var feed investmentReleaseFeed
	if err := json.Unmarshal(raw, &feed); err != nil {
		return fmt.Errorf("投资发布稿目录不是有效 JSON: %w", err)
	}
	if !feed.Success {
		return fmt.Errorf("投资发布稿目录缺少成功标记或查询失败")
	}
	if len(feed.Data) == 0 && (page == 1 || latest == "") {
		return fmt.Errorf("国家统计局投资发布稿目录为空或缺少对应月报，无法确认档案覆盖")
	}
	if len(feed.Data) > 0 && seenPages[investmentFeedSignature(feed.Data)] {
		return fmt.Errorf("投资发布稿目录返回重复分页，无法确认档案覆盖")
	}
	combined := make(map[string]industrialRelease, len(found)+len(feed.Data))
	for period, release := range found {
		combined[period] = release
	}
	candidateLatest := latest
	for _, release := range feed.Data {
		period := investmentReleasePeriod(release.Title, sector)
		if period == "" {
			continue
		}
		if period > candidateLatest {
			candidateLatest = period
		}
		if !needed[period] {
			continue
		}
		if previous, ok := combined[period]; ok && previous.URL != release.URL {
			return fmt.Errorf("投资发布稿同月存在多个版本，需核验：%s", period)
		}
		combined[period] = release
	}
	if len(feed.Data) < 100 {
		if candidateLatest == "" {
			return fmt.Errorf("官方目录未检索到相应投资月报，无法确认档案覆盖")
		}
		for period := range needed {
			if _, ok := combined[period]; !ok && period <= candidateLatest {
				return fmt.Errorf("官方发布稿目录存在档案缺口：%s %s；未用其他口径补值", sector, period)
			}
		}
	}
	if page == 100 && len(feed.Data) >= 100 && len(combined) < len(needed) {
		return fmt.Errorf("投资发布稿目录超过 100 页，请缩小查询范围或核验官方目录")
	}
	return nil
}

func investmentFeedSignature(data []industrialRelease) string {
	signature := make([]string, len(data))
	for i, release := range data {
		signature[i] = release.Title + "\n" + release.URL
	}
	sort.Strings(signature)
	return strings.Join(signature, "\n")
}
