package provider

import "testing"

func TestParseSinaSearchResults(t *testing.T) {
	body := `var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,";`
	parseSinaSearchResults(body)
}
