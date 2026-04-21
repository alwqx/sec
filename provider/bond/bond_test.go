package bond

import (
	"context"
	"fmt"
	"testing"

	"github.com/alwqx/sec/utils"
	"github.com/stretchr/testify/assert"
	"gopkg.in/h2non/gock.v1"
)

func TestGetChinaBond(t *testing.T) {
	ctx := context.TODO()

	// 1 nil req
	res, err := GetChinaBond(ctx, nil)
	assert.NotNil(t, err)
	assert.Nil(t, res)
	fmt.Println(utils.JSONify(res))

	// 2. common
	body := `{
    "heList": [
        {
            "workTime": "2023-06-02",
            "threeMonth": "1.71",
            "sixMonth": "1.85",
            "oneYear": "1.97",
            "twoYear": "2.19",
            "threeYear": "2.26",
            "fiveYear": "2.45",
            "sevenYear": "2.66",
            "tenYear": "2.70",
            "fifteenYear": "",
            "twentyYear": "",
            "thirtyYear": "3.08",
            "qxmc": "中债国债收益率曲线"
        },
        {
            "workTime": "2023-06-01",
            "threeMonth": "1.71",
            "sixMonth": "1.85",
            "oneYear": "1.97",
            "twoYear": "2.17",
            "threeYear": "2.24",
            "fiveYear": "2.43",
            "sevenYear": "2.65",
            "tenYear": "2.68",
            "fifteenYear": "",
            "twentyYear": "",
            "thirtyYear": "3.07",
            "qxmc": "中债国债收益率曲线"
        }
    ],
    "flag": "0"
}`
	defer gock.Off()
	gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").
		MatchParams(map[string]string{
			"startDate": "2023-06-01",
			"endDate":   "2023-06-02",
			"gjqx":      "0",
			"locale":    "cn_ZH",
			"qxmc":      "1",
		}).
		Reply(200).BodyString(body).
		Header.Add("content-type", "application/json;charset=UTF-8")

	req := &GetChinaBondReq{
		Start: "2023-06-01",
		End:   "2023-06-02",
	}
	res, err = GetChinaBond(ctx, req)
	assert.Nil(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, 2, len(res.HeList))
	assert.Equal(t, "0", res.Flag)
	fmt.Println(utils.JSONify(res))
}
