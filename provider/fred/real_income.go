package fred

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const realIncomeCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=DSPIC96"

type RealIncomeRequest struct{ Begin, End string }

func (r *RealIncomeRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("实际可支配个人收入查询请求不能为空")
	}
	for _, period := range []string{r.Begin, r.End} {
		if _, err := time.Parse("200601", period); err != nil || len(period) != 6 {
			return fmt.Errorf("无效月份 %q，请使用YYYYMM", period)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "195901" {
		return fmt.Errorf("实际可支配个人收入月度查询支持自195901起")
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

// RealIncomeValue 保留官方总量和原始精度；水平为季调折年率，并非当月实际收入。
type RealIncomeValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawValue string   `json:"raw_value"`
}

type RealIncomeObservation struct {
	RealIncomeValue
	YoY     *float64        `json:"yoy_percent"`
	MoM     *float64        `json:"mom_percent"`
	YoYBase RealIncomeValue `json:"yoy_base"`
	MoMBase RealIncomeValue `json:"mom_base"`
	Note    string          `json:"note,omitempty"`
}

type RealIncomeResult struct {
	Region             string                  `json:"region"`
	Indicator          string                  `json:"indicator"`
	SeriesID           string                  `json:"series_id"`
	BEAAccountCode     string                  `json:"bea_account_code"`
	Frequency          string                  `json:"frequency"`
	Measure            string                  `json:"measure"`
	Unit               string                  `json:"unit"`
	ReferenceYear      int                     `json:"reference_year"`
	SeasonallyAdjusted bool                    `json:"seasonally_adjusted"`
	LevelAnnualized    bool                    `json:"level_annualized"`
	GrowthAnnualized   bool                    `json:"growth_annualized"`
	Begin              string                  `json:"begin"`
	End                string                  `json:"end"`
	Producer           string                  `json:"producer"`
	Distributor        string                  `json:"distributor"`
	Source             string                  `json:"source"`
	SeriesSource       string                  `json:"series_source"`
	OriginalSource     string                  `json:"original_source"`
	FetchedAt          time.Time               `json:"fetched_at"`
	DataVersion        string                  `json:"data_version"`
	PublishedAt        *time.Time              `json:"published_at"`
	RevisionID         *string                 `json:"revision_id"`
	Observations       []RealIncomeObservation `json:"observations"`
	Note               string                  `json:"note"`
}

// RealIncome 直接采集官方实际税后收入总量，不用名义收入、人口或PCE自行拼接。
func (c *Client) RealIncome(ctx context.Context, req *RealIncomeRequest) (*RealIncomeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var values map[string]RealIncomeValue
	_, fetched, err := c.csvSnapshot(ctx, c.incomeEndpoint, "real-income.json", func(raw string) error {
		var err error
		values, err = parseRealIncome(raw)
		return err
	})
	if err != nil {
		return nil, err
	}
	result := &RealIncomeResult{
		Region: "US", Indicator: "real-income", SeriesID: "DSPIC96", BEAAccountCode: "A067RX",
		Frequency: "monthly", Measure: "real-disposable-personal-income", Unit: "十亿美元（2017年链式美元）",
		ReferenceYear: 2017, SeasonallyAdjusted: true, LevelAnnualized: true, GrowthAnnualized: false,
		Begin: req.Begin, End: req.End, Producer: "BEA", Distributor: "FRED", Source: realIncomeCSVURL,
		SeriesSource: "https://fred.stlouisfed.org/series/DSPIC96", OriginalSource: "https://www.bea.gov/data/income-saving/personal-income",
		FetchedAt: fetched, DataVersion: "latest-available", Observations: []RealIncomeObservation{},
		Note: "实际可支配个人收入为扣除个人当期税后的收入总量，并已扣除价格变动影响；不是人均收入、名义收入或实际个人消费支出。单位为十亿美元（2017年链式美元），月度水平采用季调折年率（SAAR），不是当月实际收入。同比和月环比由同一季调水平序列计算，增速未年化、缺口不补。历史可修订或调整参考年；CSV不提供逐期实际公布时间、修订编号和完整历史版本，published_at/revision_id=null；fetched_at仅为获取时间。",
	}
	value := func(period string) RealIncomeValue {
		if v, ok := values[period]; ok {
			return v
		}
		return RealIncomeValue{Period: period}
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		item := RealIncomeObservation{RealIncomeValue: value(month.Format("200601")), YoYBase: value(month.AddDate(-1, 0, 0).Format("200601")), MoMBase: value(month.AddDate(0, -1, 0).Format("200601"))}
		item.YoY, item.MoM = pceChange(item.Level, item.YoYBase.Level), pceChange(item.Level, item.MoMBase.Level)
		var notes []string
		if item.Level == nil {
			notes = append(notes, "当月实际可支配收入水平缺失")
		} else {
			if item.YoY == nil {
				notes = append(notes, "同比基期缺失或为零")
			}
			if item.MoM == nil {
				notes = append(notes, "环比基期缺失或为零")
			}
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

func parseRealIncome(raw string) (map[string]RealIncomeValue, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED实际收入CSV表头失败: %w", err)
	}
	if len(header) != 2 {
		return nil, fmt.Errorf("FRED实际收入CSV表头不完整")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != "DSPIC96" {
			return nil, fmt.Errorf("FRED返回未知实际收入列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("FRED返回重复列: %s", name)
		}
		columns[name] = i
	}
	values := map[string]RealIncomeValue{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED实际收入CSV失败: %w", err)
		}
		dateText := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", dateText)
		if err != nil || date.Day() != 1 || date.Format("2006-01-02") != dateText || dateText < "1959-01-01" {
			return nil, fmt.Errorf("FRED返回无效实际收入月度日期: %s", dateText)
		}
		period := date.Format("200601")
		if _, ok := values[period]; ok {
			return nil, fmt.Errorf("FRED返回重复实际收入月份: %s", period)
		}
		text := row[columns["DSPIC96"]]
		v := RealIncomeValue{Period: period, RawValue: text}
		if trimmed := strings.TrimSpace(text); trimmed != "" && trimmed != "." {
			n, err := strconv.ParseFloat(trimmed, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
				return nil, fmt.Errorf("FRED返回无效实际收入水平: %s %q", period, text)
			}
			v.Level = &n
		}
		values[period] = v
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("FRED未返回实际收入历史数据")
	}
	return values, nil
}
