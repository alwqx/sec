package bond

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/alwqx/sec/utils"
)

var chinaBondUrlTemp = `https://yield.chinabond.com.cn/cbweb-czb-web/czb/historyQuery?startDate=%s&endDate=%s&gjqx=0&locale=cn_ZH&qxmc=1`

// ChinaBondItem yield.chinabond.com.cn 网址接口返回的数据
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

// GetChinaBondReq 请求参数
type GetChinaBondReq struct {
	Start, End string
}

// GetChinaBondResp yield.chinabond.com.cn 网址接口返回的数据
type GetChinaBondResp struct {
	HeList []*ChinaBondItem `json:"heList"`
	Flag   string           `json:"flag"`
}

// GetChinaBond 请求 [start, end] 区间内的国债数据
// start 和 end 的格式为 2003-05-18，指具体某一天，且最大查询区间不超过1年
func GetChinaBond(ctx context.Context, req *GetChinaBondReq) (res *GetChinaBondResp, err error) {
	if req == nil {
		err = errors.New("req is nil")
		return
	}

	reqUrl := fmt.Sprintf(chinaBondUrlTemp, req.Start, req.End)
	headers := make(http.Header)
	headers.Add("Content-Type", "application/json;charset=UTF-8")
	headers.Add("Accept", "application/json, text/javascript, */*; q=0.01")
	headers.Add("Accept-Language", "zh-CN,zh;q=0.9")
	headers.Add("Origin", "https://yield.chinabond.com.cn")
	headers.Add("Host", "yield.chinabond.com.cn")

	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqUrl, headers, nil)
	if err != nil {
		slog.ErrorContext(ctx, "failed request", "url", reqUrl, "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	var bodyBytes []byte
	bodyBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		slog.ErrorContext(ctx, "read body failed", "url", reqUrl, "error", err)
		return nil, err
	}

	err = json.Unmarshal(bodyBytes, &res)

	return
}
