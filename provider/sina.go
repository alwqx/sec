package provider

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/carlmjohnson/requests"
)

type SinaProvider struct{}

func Search(key string) {
	var (
		res string
		err error
	)
	reqUrl := fmt.Sprintf("https://suggest3.sinajs.cn/suggest/type=11,12,15,21,22,23,24,25,26,31,33,41&key=%s", key)
	err = requests.
		URL(reqUrl).
		ContentType("application/json;charset=UTF-8").
		Accept("application/json, text/javascript, */*; q=0.01").
		Header("Accept-Language", "zh-CN,zh;q=0.9").
		Header("Origin", "https://yield.chinabond.com.cn").
		Header("Host", "yield.chinabond.com.cn").
		ToString(&res).
		Fetch(context.Background())
	if err != nil {
		slog.Error("[RequestChinaBond] request %s error: %v", reqUrl, err)
	}

	fmt.Println(res)
}
