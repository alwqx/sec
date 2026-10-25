package eastmoney

import (
	"context"
	"fmt"
	"time"
)

// ValidateIPODates 校验可选上市日期边界，允许单边查询，边界均包含。
func ValidateIPODates(since, until string) error {
	for _, value := range []string{since, until} {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return fmt.Errorf("上市日期格式无效，请使用 YYYY-MM-DD")
			}
		}
	}
	if since != "" && until != "" && since > until {
		return fmt.Errorf("开始日期不能晚于结束日期")
	}
	return nil
}

// ListIPOFiltered 先获取源列表并过滤，再进行本地分页，避免仅筛选源接口当前页。
func ListIPOFiltered(ctx context.Context, since, until string, page, pageSize int) ([]*IListing, int, error) {
	if err := ValidateIPODates(since, until); err != nil {
		return nil, 0, err
	}
	if page < 1 || pageSize < 1 {
		return nil, 0, fmt.Errorf("页码与每页条数必须大于零")
	}
	const batchSize = 500
	filtered := []*IListing{}
	for sourcePage := 1; ; sourcePage++ {
		batch, total, err := ListIPO(ctx, &IPOListReq{PageNum: sourcePage, PageSize: batchSize})
		if err != nil {
			return nil, 0, err
		}
		// 提前结束的源列表不能作为完整的筛选结果返回。
		if len(batch) == 0 && (sourcePage-1)*batchSize < total {
			return nil, 0, fmt.Errorf("新股列表返回不完整")
		}
		for _, item := range batch {
			if item == nil {
				continue
			}
			date, err := time.Parse("20060102", item.ListingDate)
			if err != nil {
				continue
			} // 日期不明的记录不能判断是否落入筛选区间。
			value := date.Format("2006-01-02")
			if since != "" && value < since || until != "" && value > until {
				continue
			}
			filtered = append(filtered, item)
		}
		if sourcePage*batchSize >= total {
			break
		}
		if len(batch) < batchSize {
			return nil, 0, fmt.Errorf("新股列表返回不完整")
		}
	}
	total := len(filtered)
	start := (page - 1) * pageSize
	if start >= total {
		return []*IListing{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return filtered[start:end], total, nil
}
