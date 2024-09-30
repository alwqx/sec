package provider

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type SinaProvider struct{}

func Search(key string) {
	var (
		err error
	)
	reqUrl := fmt.Sprintf("https://suggest3.sinajs.cn/suggest/type=11,12,15,21,22,23,24,25,26,31,33,41&key=%s", key)

	resp, err := http.DefaultClient.Get(reqUrl)
	if err != nil {
		slog.Error("[Search] request %s error: %v", reqUrl, err)
		return
	}
	defer resp.Body.Close()

	var resBytes []byte
	resBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("[Search] request %s error: %v", reqUrl, err)
		return
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "charset=GBK") {
		resBytes, err = simplifiedchinese.GBK.NewDecoder().Bytes(resBytes)
	}
	if err != nil {
		slog.Error("[Search] request %s error: %v", reqUrl, err)
		return
	}

	fmt.Println(string(resBytes))
}
