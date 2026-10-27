// Package chinabond 采集中债官方国债收益率曲线。
package chinabond

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const CoverageStart = "20060301"

// ChinaBondItem 恢复PR #66删除的官方日期行结构，空字符串和null字段均表示未提供值。
type ChinaBondItem struct {
	Date        string `json:"workTime"`
	ThreeMonth  string `json:"threeMonth"`
	SixMonth    string `json:"sixMonth"`
	OneYear     string `json:"oneYear"`
	TwoYear     string `json:"twoYear"`
	ThreeYear   string `json:"threeYear"`
	FiveYear    string `json:"fiveYear"`
	SevenYear   string `json:"sevenYear"`
	TenYear     string `json:"tenYear"`
	FifteenYear string `json:"fifteenYear"`
	TwentyYear  string `json:"twentyYear"`
	ThirtyYear  string `json:"thirtyYear"`
	Qxmc        string `json:"qxmc"`
}

// GetChinaBondReq 起止日期为YYYY-MM-DD，单次区间不超过一年。
type GetChinaBondReq struct{ Start, End string }

type GetChinaBondResp struct {
	HeList []*ChinaBondItem `json:"heList"`
	Flag   string           `json:"flag"`
}

// GetChinaBond 恢复PR #66中的GET historyQuery采集接口。
func GetChinaBond(ctx context.Context, req *GetChinaBondReq) (*GetChinaBondResp, error) {
	if req == nil {
		return nil, fmt.Errorf("国债收益率查询请求不能为空")
	}
	start, err := time.Parse("2006-01-02", req.Start)
	if err != nil {
		return nil, fmt.Errorf("无效开始日期: %s", req.Start)
	}
	end, err := time.Parse("2006-01-02", req.End)
	if err != nil {
		return nil, fmt.Errorf("无效截至日期: %s", req.End)
	}
	if start.After(end) || !end.Before(start.AddDate(1, 0, 0)) {
		return nil, fmt.Errorf("单次国债查询区间需按日期升序且小于一年")
	}
	raw, err := requestChinaBond(ctx, chinaBondURL("https://yield.chinabond.com.cn", req))
	if err != nil {
		return nil, err
	}
	result, err := decodeChinaBond(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, item := range result.HeList {
		if item == nil {
			return nil, fmt.Errorf("中债返回空数据行")
		}
		date, err := time.Parse("2006-01-02", item.Date)
		if err != nil || date.Before(start) || date.After(end) || seen[item.Date] {
			return nil, fmt.Errorf("中债所属日期无效、越界或重复: %s", item.Date)
		}
		seen[item.Date] = true
		for _, text := range []string{item.ThreeMonth, item.SixMonth, item.OneYear, item.TwoYear, item.FiveYear, item.TenYear} {
			if strings.TrimSpace(text) == "" {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("中债收益率无效: %s", text)
			}
		}
	}
	return result, nil
}

func chinaBondURL(origin string, req *GetChinaBondReq) string {
	query := url.Values{"startDate": {req.Start}, "endDate": {req.End}, "gjqx": {"0"}, "locale": {"cn_ZH"}, "qxmc": {"1"}}
	return origin + "/cbweb-czb-web/czb/historyQuery?" + query.Encode()
}

func decodeChinaBond(raw []byte) (*GetChinaBondResp, error) {
	var result *GetChinaBondResp
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("中债响应不是有效JSON: %w", err)
	}
	if result == nil || result.Flag != "0" || result.HeList == nil {
		return nil, fmt.Errorf("中债响应失败或缺少heList/flag字段")
	}
	return result, nil
}

func requestChinaBond(ctx context.Context, source string) ([]byte, error) {
	headers := make(http.Header)
	headers.Set("User-Agent", utils.UserAgent())
	headers.Set("Content-Type", "application/json;charset=UTF-8")
	headers.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	headers.Set("Accept-Language", "zh-CN,zh;q=0.9")
	headers.Set("Origin", "https://yield.chinabond.com.cn")
	slog.DebugContext(ctx, "请求中债国债收益率曲线", "url", source)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, source, headers, nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("中债返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, fmt.Errorf("中债响应超过大小限制")
	}
	return raw, nil
}
