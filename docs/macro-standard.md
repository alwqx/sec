# 宏观数据统一导出

`--standard-json` 提供版本化的宏观序列 JSON，已接入 CN M1/M2、US M2、CN 社零和房地产，以及 US GDP、EFFR。采集仍使用原来的人民银行、FRED、NBS、纽约联储 provider 和缓存，不增加网络请求。

```bash
sec macro cn money --begin 202412 --end 202502 --standard-json
sec macro us money --begin 202501 --end 202609 --standard-json
sec macro cn retail --begin 202601 --end 202609 --standard-json
sec macro cn property --component sales-area --begin 202601 --end 202609 --standard-json
sec macro us gdp --begin 2025Q1 --end 2026Q2 --standard-json
sec macro us gdp --begin 2026Q2 --end 2026Q2 --vintages --standard-json
sec macro us effr --begin 20250102 --end 20250106 --standard-json
```

各命令分别输出独立数据集。原有表格、别名、日期默认值、`--json`、`--refresh` 和 `--no-cache` 行为不变；`--json` 与 `--standard-json` 互斥。统一字段用于程序读取，不改变币种、单位或统计口径，也不自动生成中美对比。

## 契约版本 1

顶层包含 `schema_version="1"`、`region`、`begin/end`、`series`、`observations` 和 `original`。月度所属期使用 YYYYMM，季度使用 YYYYQ1 至 YYYYQ4，日度使用 YYYYMMDD；每份数据集只包含一种频率，查询端点、观测日期按该频率校验。每个观测只包含一个值，通过 `series_id` 关联序列。消费者应检查版本、频率，并用序列 ID 和所属期作为联合键。

`series` 为实际返回观测所引用的序列定义，包含：

| 字段 | 含义 |
| --- | --- |
| `id`、`indicator` | 序列标识、指标，例如 `us.m2.level`、`m2` |
| `measure` | `level`、`yoy_percent`、`mom_percent`、`ytd_level`、`ytd_yoy_percent`；GDP 另有 `qoq_percent`、`qoq_annualized_percent`；EFFR 为 `rate`、`previous_observation_change` |
| `unit` | `CNY_100_million`（亿元人民币）、`USD_billion`（十亿美元）、`square_meters_10000`（万平方米）、`percent`、`basis_points`（基点） |
| `frequency` | `monthly`、`quarterly`、`daily` |
| `period_basis` | CN 货币为 `month-end`；US 货币为 `monthly-average`；社零单月为 `month`，社零和房地产累计为 `year-to-date`；GDP 为 `quarter`；EFFR 为 `overnight-transaction-date` |
| `seasonal_adjustment` | CN、EFFR 为 `not-seasonally-adjusted`；US M2、GDP 为 `seasonally-adjusted` |
| `price_basis` | 货币和社零、房地产金额为 `nominal`；实际GDP为 `real-chained`；房屋面积和利率为 `not_applicable` |
| `annualized` | 水平与增速各自标记；GDP 水平和官方环比年化增速为 true，其余当前序列为 false |
| `value_origin` | `official` 或 `derived`，不以计算值冒充官方公布值 |
| `definition`、`formula` | 统计口径标识；计算增速保留公式，官方值公式为 null |

CN 官方同比直接保留，不用展示余额重新计算。M1 在 2025 年 1 月处分成 `cn.m1.original.*` 与 `cn.m1.revised-2025.*`；M2 为 `cn.m2.pbc-m2.*`。M1 修订前后不共用序列 ID，不计算跨口径变化。US 使用 `us.m2.level`、`us.m2.yoy_percent`、`us.m2.mom_percent`，定义为 `M2SL`；同比和月环比为计算值。

### 社零与房地产的累计所属期

社零的 `cn.retail.level`、`cn.retail.yoy_percent` 为官方单月值，`cn.retail.ytd_level`、`cn.retail.ytd_yoy_percent` 为官方年初累计值。四条序列都是全国总额、名义口径、未季调，不混入限额以上单位。

房地产使用 `cn.property.<component>.<scope_version>.<measure>`，支持 `sales-area`、`sales-value`、`starts-area`。销售金额为亿元，面积为万平方米；销售序列按 provider 的2005年8月前、过渡月、后续范围版本拆开，房屋新开工采用自己的范围版本。原始定义、注释、目录与覆盖起点保留在 `original` 中。

这两个NBS导出为每条观测增加 `period_begin`、`period_end`（YYYYMM）和 `source_series_id`。这些是版本1的可选扩展字段，货币导出保持原结构。单月值的起止期相同；累计值从当年1月开始，累计所属期必须与 provider 的 `ytd_period` 一致，否则报错。

例如2月社零的累计金额标记为 `period="202602"`、`period_begin="202601"`、`period_end="202602"`。该金额不放入2月单月序列，也不分摊给1月。若 provider 返回1月空值，保留空观测；若没有返回1月，不额外生成一行。累计同比直接保留官方值，既不相减生成单月同比，也不通过与去年发布金额相除生成替代值。跨年累计所属期从新年度1月重新开始，不对年度累计求和。

### 美国 GDP：季度与折年口径

GDP 提供四条独立序列：`us.gdp.level` 为 GDPC1 官方实际水平，十亿美元、2017年链式美元、季调折年（SAAR），不是季度实际金额；`us.gdp.qoq_annualized_percent` 直接保留 A191RL1Q225SBEA 官方季调环比年化增速，不由当前水平重算替代。`us.gdp.qoq_percent`、`us.gdp.yoy_percent` 保留 provider 由同一当前快照严格上季／上年同季水平计算的未年化增速及基期。

GDP 序列使用可选 `reference_year=2017`，`source_series_id` 区分水平与官方增长序列。`raw_value` 保留官方水平／年化增速的CSV字符串，计算增速为 null。官方年化增速独立保留，即使同季度实际水平缺失也不随之丢弃。

`--vintages --standard-json` 仍采集BEA发布档案，完整放入 `original` 的各季度 `vintages` 中；顶层规范观测仍只有当前版本的四条序列，不把初值或后续版本混入相同联合键。档案真实发布日期保留原精度，历史实际水平仍为 null，不以档案的名义金额或当前修订水平替代。该出口不提供统一的历史 as-of 查询。

### EFFR：交易所属日与方法边界

EFFR仅返回官方实际观测，日期从ISO交易所属日转换为YYYYMMDD，不补周末、节假日或未返回的日期。官方利率的单位为 `percent`，相对前次实际观测的计算差为 `basis_points`，不称为相邻自然日变化，也不对利率计算百分比增速。

序列 ID 为 `us.effr.<method>.<measure>`。2016-03-01前的方法为 `broker-volume-weighted-mean`，之后为 `fr2420-volume-weighted-median`，`definition` 保留 provider 的原方法说明。跨方法边界的变化保留 null，缺失原因 `methodology_break`；没有前次观测时为 `previous_observation_missing`。零利率、零变化都是有效值。

计算变化的 `base.period` 保留前次交易日。前值在当前结果中时保留其水平；前值在窗口之外时，provider只返回前次日期和变化量，因此 `base.value=null` 并在 `note` 中说明，不从变化量反推。已有的计算变化仍可用，缺失的基期水平不代表变化量缺失。需要直接查看前值时应扩大请求区间。

EFFR非空修订标记用可选 `revision_indicator` 保留，不将其冒充 `revision_id`。每条原始API记录（含分位数、成交量等）、交易日期和原方法字段都保留在 `original`；没有独立原始数字字符串，标准观测的 `raw_value=null`。逐条公布时间不可获得时保持 null。

## 缺失与溯源

每条观测保留 `period`（按频率使用YYYYMM、YYYYQn或YYYYMMDD）、`value`、`status`、`missing_reason`、`raw_value`、`source`、`fetched_at`、`published_date`、`published_at`、`revision_id`、`data_version`、`base`、`note`。

- 数值存在时 `status="available"`，包括零值和负增速；`missing_reason=null`。
- 数值缺失时 `value=null`、`status="missing"`。`report_not_found` 表示人民银行当前目录未检索到月份；`source_value_missing` 表示源水平或报告字段缺失；计算增速另外区分 `current_value_missing`、`baseline_missing`、`zero_baseline`，其他无法计算的情况为 `calculation_unavailable`。缺失不自动归因于未发布、停更或历史不存在。
- US 计算值的 `base` 包含严格上一月／上年同月的所属期、水平和原始字符串，来自同份快照；水平和 CN 官方同比的 `base=null`。不重新计算、不跨缺口寻找替代基期。
- US 官方水平的 `raw_value` 保留 CSV 原始字符串，包括缺失标记；计算增速的原始字符串为 null。当前接入的 CN provider 未提供逐字段原始数字字符串，该字段为 null，不把格式化后的数值冒充原值。NBS逐字段来源标识另存为 `source_series_id`。
- `source` 和 `fetched_at` 保留 provider 实际来源与抓取时刻，命中缓存时保留原抓取时间。未找到 CN 报告时两者均为 null，不输出公元 1 年的虚假时间。
- CN `published_date` 保留报告原有发布日期字段，仅表示源日期及其原精度；不填充零点作为精确公布时间。无法取得的 `published_at`、`revision_id` 为 null。
- `data_version="current_visible_revision"` 表示查询时当前可见历史；不能据此进行“当时已知”的历史回测，也不提供完整 vintage 档案。US 原 provider 的版本标签另外保存在 `original` 中。

`original` 是适配前的完整 provider JSON 结果，保留原统计注释、方法变化、基期和来源字段。它**不是上游 HTTP 原始响应**；原始 HTML/CSV 的缓存策略仍由 provider 管理。新增适配不会改写原 provider 结果。

网络、解析、编码失败直接报错，不输出部分成功的数据集；状态字段不用于把请求失败伪装为数据缺失。当前不增加数据过期判定，沿用 provider 六小时缓存与失败处理；`available` 仅表示该观测有数值，不承诺它是最新公布期。

## 覆盖与后续迁移

当前完成货币指标、社零、房地产与美国GDP、EFFR的统一导出，未将所有宏观 provider 迁移。其余指标继续使用原 JSON，TODO 中全局基础项仍保留未完成状态。其他日度／季度／累计指标、部分抓取失败、完整历史版本与中美可比性需要各自的适配和测试，不因本批格式扩展而视为已完成。

测试覆盖真实离线样本、旧输出回归、官方值／计算值、M1口径边界、房地产范围变化、累计所属期、1—2月合并、零与缺失、严格基期、空结果、元数据空值、重复月份、非法范围、非有限值、上游错误、取消及输出写入失败。
