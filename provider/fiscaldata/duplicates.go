package fiscaldata

import (
	"fmt"
	"strconv"
)

func amountKey(values ...*float64) string {
	key := ""
	for _, value := range values {
		if value == nil {
			key += "null/"
		} else {
			key += strconv.FormatFloat(*value, 'g', -1, 64) + "/"
		}
	}
	return key
}

// uniqueRows 只合并标签和金额都相同的上游重复行，金额不同的同标签行保留，由调用方决定如何处理。
// lines 按保留行的下标记录全部源行号，仅包含确有重复的行。
func uniqueRows[T any](rows []T, key func(T) string, line func(T) string) ([]T, map[int][]string) {
	unique, lines, index := []T{}, map[int][]string{}, map[string]int{}
	for _, row := range rows {
		first, repeated := index[key(row)]
		if !repeated {
			index[key(row)] = len(unique)
			unique = append(unique, row)
			continue
		}
		if len(lines[first]) == 0 {
			lines[first] = []string{line(unique[first])}
		}
		lines[first] = append(lines[first], line(row))
	}
	return unique, lines
}

// UniqueDebtRows 合并DTS表IIIA中内容完全相同的重复行，例如2007-02-06。
func UniqueDebtRows(rows []DebtRow) ([]DebtRow, map[int][]string) {
	return uniqueRows(rows, func(r DebtRow) string {
		return fmt.Sprintf("%s/%s/%s/%s/%s/%s", r.Date, r.Transaction, r.Market, r.Security, r.Description, amountKey(r.Today, r.MTD, r.FYTD))
	}, func(r DebtRow) string { return r.Raw["src_line_nbr"] })
}

// UniqueCashRows 合并同一记录日内容完全相同的IIIB调整明细。
func UniqueCashRows(rows []CashRow) ([]CashRow, map[int][]string) {
	return uniqueRows(rows, func(r CashRow) string {
		return fmt.Sprintf("%s/%s/%s/%s/%s", r.Date, r.Transaction, r.Category, r.Description, amountKey(r.Today, r.MTD, r.FYTD))
	}, func(r CashRow) string { return r.Raw["src_line_nbr"] })
}
