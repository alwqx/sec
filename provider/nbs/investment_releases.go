package nbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/utils"
)

var investmentTitlePattern = regexp.MustCompile(`^(\d{4})年(?:1[—－–\-至](\d{1,2})月份)?全国(固定资产投资|房地产(?:开发投资和销售|开发和销售|市场))`)
var investmentTotalPattern = regexp.MustCompile(`全国固定资产投资\(不含农户\)([0-9]+(?:\.[0-9]+)?)亿元[，,](?:同比|比上年同期|比上年)?(增长|下降)([0-9]+(?:\.[0-9]+)?)%`)
var investmentInfrastructurePattern = regexp.MustCompile(`基础设施投资(?:\(([^)]*)\))?(?:同比|比上年同期|比上年)?(增长|下降)([0-9]+(?:\.[0-9]+)?)%`)

func investmentReleasePeriod(title, sector string) string {
	m := investmentTitlePattern.FindStringSubmatch(compact(title))
	if m == nil {
		return ""
	}
	property := strings.HasPrefix(m[3], "房地产")
	if property != (sector == "real-estate") {
		return ""
	}
	month := 12
	if m[2] != "" {
		month, _ = strconv.Atoi(m[2])
	}
	if month < 2 || month > 12 {
		return ""
	}
	return fmt.Sprintf("%s%02d", m[1], month)
}

func investmentText(text string) string {
	return strings.NewReplacer("（", "(", "）", ")", "％", "%", "−", "-").Replace(compact(text))
}

// 原始稿须先通过当期及所选分项校验再写缓存，不从过期缓存降级返回。
func (c *Client) investmentRelease(ctx context.Context, release industrialRelease, sector, period string) (*InvestmentObservation, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	u, err := url.Parse(release.URL)
	if err != nil || u.Scheme != "https" || u.Host != "www.stats.gov.cn" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/sj/") {
		return nil, time.Time{}, fmt.Errorf("无效的国家统计局投资发布稿链接")
	}
	key := sha256.Sum256([]byte("investment-release\n" + release.URL))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var entry cacheEntry
			var html string
			if json.Unmarshal(raw, &entry) == nil && time.Since(entry.FetchedAt) >= 0 && time.Since(entry.FetchedAt) < 6*time.Hour && json.Unmarshal(entry.Body, &html) == nil {
				if item, err := parseInvestmentRelease(html, sector, period); err == nil {
					return item, entry.FetchedAt, nil
				}
			}
		}
	}
	headers := http.Header{}
	headers.Set("User-Agent", utils.UserAgent())
	resp, err := utils.MakeRequest(ctx, http.MethodGet, release.URL, headers, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("请求投资发布稿失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("投资发布稿返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 4<<20 {
		return nil, time.Time{}, fmt.Errorf("投资发布稿超过大小限制")
	}
	item, err := parseInvestmentRelease(string(raw), sector, period)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("解析投资发布稿失败（%s）: %w", release.URL, err)
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		body, _ := json.Marshal(string(raw))
		if err := c.saveCache(path, cacheEntry{FetchedAt: fetched, Body: body}); err != nil {
			return nil, time.Time{}, err
		}
	}
	return item, fetched, nil
}

func parseInvestmentRelease(html, sector, period string) (*InvestmentObservation, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	title, ok := doc.Find(`meta[name="ArticleTitle"]`).Attr("content")
	if !ok || investmentReleasePeriod(title, sector) != period {
		return nil, fmt.Errorf("投资发布稿标题与请求月份或分项不符")
	}
	body := doc.Find(".trs_editor_view, .TRS_Editor").First()
	if body.Length() == 0 {
		return nil, fmt.Errorf("投资发布稿缺少正文")
	}
	body.Find("style,script").Remove()
	text := investmentText(body.Text())
	item := &InvestmentObservation{Region: "CN", Sector: sector, Period: period, YTDPeriod: period[:4] + "01-" + period, AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", Status: "published", Note: "当期官方发布稿值；未提供可回溯历史版本接口；累计同比不相减推算单月同比"}
	if published, ok := doc.Find(`meta[name="PubDate"]`).Attr("content"); ok && strings.TrimSpace(published) != "" {
		value, err := time.ParseInLocation("2006/01/02 15:04", strings.TrimSpace(published), time.FixedZone("CST", 8*60*60))
		if err != nil {
			return nil, fmt.Errorf("投资发布稿公布时间无效: %s", published)
		}
		item.PublishedAt = &value
	} else {
		item.Note += "；发布稿未提供公布时间"
	}
	if strings.HasSuffix(period, "02") {
		item.Note += "；1—2月合并累计，不是2月单月值"
	}
	body.Find("p").Each(func(_ int, p *goquery.Selection) {
		ptext := investmentText(p.Text())
		if strings.Contains(ptext, "上年同期") && (strings.Contains(ptext, "修订") || strings.Contains(ptext, "可比口径")) && !strings.Contains(item.BasisNote, ptext) {
			if item.BasisNote != "" {
				item.BasisNote += "；"
			}
			item.BasisNote += ptext
		}
	})
	if item.BasisNote == "" {
		item.BasisNote = "同比按当期官方发布值保留；未提供可回溯的基期修订版本"
	}
	switch sector {
	case "total":
		m := investmentTotalPattern.FindStringSubmatch(text)
		if m == nil {
			return nil, fmt.Errorf("月报未提供可解析的全国固定资产投资金额和累计增速")
		}
		amount, err := investmentNumber(m[1])
		if err != nil {
			return nil, err
		}
		growth, err := investmentDirectionalRate(m[2], m[3])
		if err != nil {
			return nil, err
		}
		_, tableGrowth, err := investmentTable(body, "固定资产投资(不含农户)", false)
		if err != nil {
			return nil, err
		}
		if *growth != *tableGrowth {
			return nil, fmt.Errorf("投资总量正文与数据表累计增速冲突")
		}
		item.YTDAmount, item.YTDGrowth = amount, growth
		item.Scope = "全国固定资产投资(不含农户)：计划总投资500万元及以上固定资产项目及所有房地产开发项目"
	case "manufacturing":
		_, growth, err := investmentTable(body, "制造业", false)
		if err != nil {
			return nil, err
		}
		item.YTDGrowth = growth
		item.Scope = "全国制造业固定资产投资；行业分类按当期官方月报"
		item.Note += "；官方月报未公布制造业累计金额"
	case "infrastructure":
		m := investmentInfrastructurePattern.FindStringSubmatch(text)
		if m == nil {
			return nil, fmt.Errorf("月报未提供可解析的基础设施投资累计增速")
		}
		growth, err := investmentDirectionalRate(m[2], m[3])
		if err != nil {
			return nil, err
		}
		item.YTDGrowth = growth
		switch {
		case strings.Contains(m[1], "不含电力、热力、燃气及水生产和供应业"):
			item.InfrastructureScope = "excluding_electricity_heat_gas_water"
			item.Scope = "基础设施投资(不含电力、热力、燃气及水生产和供应业)"
		case strings.Contains(text, "基础设施投资包括电力、热力生产和供应业，燃气生产和供应业，水的生产和供应业"):
			item.InfrastructureScope = "including_electricity_heat_gas_water"
			item.Scope = "基础设施投资(含电力、热力、燃气及水生产和供应业；完整行业范围见当期来源附注)"
		default:
			return nil, fmt.Errorf("基础设施投资统计范围无法确认，未将不同口径拼接")
		}
		item.Note += "；官方月报未公布基建累计金额；含电力与不含电力口径不可直接比较"
	case "real-estate":
		amount, growth, err := investmentTable(body, "房地产开发投资(亿元)", true)
		if err != nil {
			return nil, err
		}
		item.YTDAmount, item.YTDGrowth = amount, growth
		item.Scope = "全国房地产开发投资；房地产开发经营业法人单位开发项目；与按行业分的房地产业固定资产投资不同"
	default:
		return nil, fmt.Errorf("无效投资分项: %s", sector)
	}
	return item, nil
}

func investmentNumber(text string) (*float64, error) {
	raw, _ := json.Marshal(text)
	value, err := parseNumber(raw)
	if err != nil || value == nil {
		return nil, fmt.Errorf("投资月报数值无效: %q", text)
	}
	return value, nil
}

func investmentDirectionalRate(direction, value string) (*float64, error) {
	number, err := investmentNumber(value)
	if err != nil {
		return nil, err
	}
	if *number < 0 {
		return nil, fmt.Errorf("投资月报增减方向与数值矛盾")
	}
	if direction == "下降" {
		*number = -*number
	}
	return number, nil
}

// 仅取名称和单位均匹配的官方表格行；允许桌面/移动重复表，但拒绝值冲突。
func investmentTable(body *goquery.Selection, name string, withAmount bool) (*float64, *float64, error) {
	var amount, growth *float64
	var parseErr error
	body.Find("table").Each(func(_ int, table *goquery.Selection) {
		if header := investmentText(table.Text()); !strings.Contains(header, "同比增长(%)") && !strings.Contains(header, "比上年增长(%)") {
			return
		}
		table.Find("tr").Each(func(_ int, row *goquery.Selection) {
			if parseErr != nil {
				return
			}
			var cells []string
			row.ChildrenFiltered("td,th").Each(func(_ int, cell *goquery.Selection) { cells = append(cells, investmentText(cell.Text())) })
			if len(cells) == 0 || cells[0] != name {
				return
			}
			want := 2
			if withAmount {
				want = 3
			}
			if len(cells) != want {
				parseErr = fmt.Errorf("投资月报表格列数变化: %s", name)
				return
			}
			value, err := investmentNumber(cells[len(cells)-1])
			if err != nil {
				parseErr = err
				return
			}
			if growth != nil && *growth != *value {
				parseErr = fmt.Errorf("投资月报表格数值冲突: %s", name)
				return
			}
			growth = value
			if withAmount {
				value, err = investmentNumber(cells[1])
				if err != nil || (value != nil && *value < 0) {
					parseErr = fmt.Errorf("投资累计金额无效: %s", cells[1])
					return
				}
				if amount != nil && *amount != *value {
					parseErr = fmt.Errorf("投资月报表格金额冲突: %s", name)
					return
				}
				amount = value
			}
		})
	})
	if parseErr != nil {
		return nil, nil, parseErr
	}
	if growth == nil {
		return nil, nil, fmt.Errorf("投资月报缺少名称、单位匹配的数据行: %s", name)
	}
	return amount, growth, nil
}
