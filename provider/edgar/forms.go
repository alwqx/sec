package edgar

import "strings"

// formNames 是常见申报类型的中文名称。
var formNames = map[string]string{
	"10-K": "年报", "10-Q": "季报", "8-K": "重大事项", "20-F": "外国公司年报", "6-K": "外国公司临时报告", "40-F": "加拿大公司年报",
	"DEF 14A": "股东大会委托书", "DEFA14A": "委托书补充材料", "PRE 14A": "委托书初稿", "S-1": "招股书", "F-1": "外国公司招股书",
	"S-3": "简式注册书", "S-8": "员工持股计划注册书", "424B2": "招股书补充", "424B3": "招股书补充", "424B4": "招股书定稿", "424B5": "招股书补充",
	"FWP": "自由撰写招股说明书", "3": "内部人初始持股", "4": "内部人交易", "5": "内部人年度持股", "144": "拟出售限售股",
	"SC 13G": "大股东持股（被动）", "SC 13D": "大股东持股（主动）", "13F-HR": "机构持仓", "11-K": "员工福利计划年报", "SD": "冲突矿产报告",
	"10-KT": "过渡期年报", "NT 10-K": "年报延期通知", "NT 10-Q": "季报延期通知",
}

// itemNames 是 8-K 常见事项的中文名称，编号见 https://www.sec.gov/answers/form8k.htm。
var itemNames = map[string]string{
	"1.01": "签订重大协议", "1.02": "终止重大协议", "1.03": "破产", "1.05": "网络安全事件",
	"2.01": "完成收购或出售资产", "2.02": "经营业绩", "2.03": "新增重大债务", "2.05": "退出或处置成本", "2.06": "资产减值",
	"3.01": "退市或不再符合上市条件", "3.02": "未注册的股票发行", "3.03": "股东权利变更",
	"4.01": "更换审计师", "4.02": "前期财报不可依赖",
	"5.01": "控制权变更", "5.02": "董事或高管变动", "5.03": "章程或财年变更", "5.07": "股东大会表决结果", "5.08": "股东提名",
	"7.01": "公平披露（Reg FD）", "8.01": "其他事项", "9.01": "财务报表与附件",
}

// Describe 返回申报类型的中文说明；修订版加"（修订）"，8-K 附上事项名称，不认识的类型返回空字符串。
func Describe(form, items string) string {
	base, amended := strings.CutSuffix(form, "/A")
	name := formNames[base]
	if name == "" {
		return ""
	}
	if amended {
		name += "（修订）"
	}
	if base == "8-K" && items != "" {
		parts := []string{}
		for _, item := range strings.Split(items, ",") {
			// 9.01 只是附件清单，几乎每份 8-K 都有，不单独列出。
			if item = strings.TrimSpace(item); item != "9.01" && itemNames[item] != "" {
				parts = append(parts, itemNames[item])
			}
		}
		if len(parts) > 0 {
			name += "：" + strings.Join(parts, "、")
		}
	}
	return name
}
