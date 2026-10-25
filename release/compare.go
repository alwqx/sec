package release

import (
	"regexp"
	"strings"
)

var semver = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// Compare 比较语义版本；开发版本无法比较时返回 known=false，避免误报更新或已是最新。
func numeric(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func Compare(current, latest string) (order int, known bool) {
	a, b := semver.FindStringSubmatch(current), semver.FindStringSubmatch(latest)
	if a == nil || b == nil {
		return 0, false
	}
	for _, parts := range [][]string{a, b} {
		for _, pre := range strings.Split(parts[4], ".") {
			if numeric(pre) && len(pre) > 1 && pre[0] == '0' {
				return 0, false
			}
		}
	}
	for index := 1; index <= 3; index++ {
		if len(a[index]) != len(b[index]) {
			if len(a[index]) < len(b[index]) {
				return -1, true
			}
			return 1, true
		}
		if result := strings.Compare(a[index], b[index]); result != 0 {
			return result, true
		}
	}
	if a[4] == b[4] {
		return 0, true
	}
	if a[4] == "" {
		return 1, true
	}
	if b[4] == "" {
		return -1, true
	}
	left, right := strings.Split(a[4], "."), strings.Split(b[4], ".")
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] == right[index] {
			continue
		}
		le, re := numeric(left[index]), numeric(right[index])
		if le && re {
			if len(left[index]) != len(right[index]) {
				if len(left[index]) < len(right[index]) {
					return -1, true
				}
				return 1, true
			}
		} else if le {
			return -1, true
		} else if re {
			return 1, true
		}
		return strings.Compare(left[index], right[index]), true
	}
	if len(left) < len(right) {
		return -1, true
	}
	return 1, true
}
