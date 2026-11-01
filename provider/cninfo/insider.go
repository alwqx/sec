package cninfo

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// insiderMaxPages 限制每个关键字的翻页数，超过时结果标记为截断。
const insiderMaxPages = 10

var insiderKeywords = []string{"增持", "减持"}

// Beijing 是公告日期使用的时区，不随本机时区变化。
var Beijing = time.FixedZone("CST", 8*3600)

var titleTagPattern = regexp.MustCompile(`<[^>]*>`)

// CleanTitle 去掉 CNINFO 搜索结果标题里的高亮标签和 HTML 转义。
func CleanTitle(title string) string {
	return html.UnescapeString(titleTagPattern.ReplaceAllString(title, ""))
}

// QueryInsiderAnnouncements 查询标题含“增持”或“减持”的有效公告，按公告时间倒序排列。
// CNINFO 的关键字搜索是模糊匹配，这里按标题严格过滤并去重；返回的 Title 仍是原文，展示前用 CleanTitle 清理。
// 结果基于标题，不是结构化的交易记录，也可能是公司增持其他公司或股东承诺。
// truncated 为 true 时某个关键字超过翻页上限，结果不完整，部分年份可能缺失。stockCode 格式为 "代码,orgId"。
func QueryInsiderAnnouncements(ctx context.Context, stockCode string) ([]*Announcement, bool, error) {
	type result struct {
		items     []*Announcement
		truncated bool
		err       error
	}
	results := make([]result, len(insiderKeywords))
	var group sync.WaitGroup
	for i, keyword := range insiderKeywords {
		group.Add(1)
		go func() {
			defer group.Done()
			items, truncated, err := queryKeywordPages(ctx, stockCode, keyword)
			results[i] = result{items, truncated, err}
		}()
	}
	group.Wait()

	items := []*Announcement{}
	truncated := false
	seen := map[string]bool{}
	for i, result := range results {
		if result.err != nil {
			return nil, false, fmt.Errorf("查询%s公告失败: %w", insiderKeywords[i], result.err)
		}
		truncated = truncated || result.truncated
		for _, item := range result.items {
			if item == nil || item.ExistFlag != 0 || item.InvalidationFlag != 0 || seen[item.ID] {
				continue
			}
			title := CleanTitle(item.Title)
			if !strings.Contains(title, "增持") && !strings.Contains(title, "减持") {
				continue
			}
			seen[item.ID] = true
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Time > items[j].Time })
	return items, truncated, nil
}

func queryKeywordPages(ctx context.Context, stockCode, keyword string) ([]*Announcement, bool, error) {
	items := []*Announcement{}
	for page := 1; page <= insiderMaxPages; page++ {
		resp, err := QueryAnnouncements(ctx, &QueryRequest{StockCode: stockCode, SearchKey: keyword, PageNum: page, PageSize: 30})
		if err != nil {
			return nil, false, err
		}
		if resp == nil || len(resp.Data) == 0 {
			return items, false, nil
		}
		items = append(items, resp.Data...)
		if !resp.HasMore && page >= resp.TotalPages {
			return items, false, nil
		}
	}
	return items, true, nil
}
