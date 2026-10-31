# 宏观数据统一导出

`--standard-json` 提供版本化的宏观序列 JSON。已接入指标及各自口径见下文，包括货币、消费、就业、生产、财政、企业部门、信用、投资、住宅、贸易、政策利率和国债交易。采集仍使用原provider及缓存，不增加网络请求。

```bash
sec macro cn money --begin 202412 --end 202502 --standard-json
sec macro us money --begin 202501 --end 202609 --standard-json
sec macro cn retail --begin 202601 --end 202609 --standard-json
sec macro cn property --component sales-area --begin 202601 --end 202609 --standard-json
sec macro us gdp --begin 2025Q1 --end 2026Q2 --standard-json
sec macro us gdp --begin 2026Q2 --end 2026Q2 --vintages --standard-json
sec macro us effr --begin 20250102 --end 20250106 --standard-json
sec macro us cpi --begin 202501 --end 202609 --standard-json
sec macro us core-cpi --begin 202501 --end 202609 --standard-json
sec macro us initial-claims --begin 20261001 --end 20261024 --standard-json
sec macro us continued-claims --begin 20261001 --end 20261024 --standard-json
sec macro us pce --begin 202501 --end 202608 --standard-json
sec macro us core-pce --begin 202501 --end 202608 --standard-json
sec macro us real-pce --component goods --begin 202501 --end 202608 --standard-json
sec macro us real-income --begin 202501 --end 202608 --standard-json
sec macro us industrial-production --sector manufacturing --begin 202501 --end 202609 --standard-json
sec macro us capacity-utilization --sector total --begin 202501 --end 202609 --standard-json
sec macro us unemployment --begin 202501 --end 202609 --standard-json
sec macro us participation --begin 202501 --end 202609 --standard-json
sec macro us earnings --begin 202501 --end 202609 --standard-json
sec macro us weekly-hours --begin 202501 --end 202609 --standard-json
sec macro us fiscal --begin 202609 --end 202610 --standard-json
sec macro us corporate-profits --begin 2025Q1 --end 2026Q2 --standard-json
sec macro us business-inventories --begin 202501 --end 202608 --standard-json
sec macro us new-orders --begin 202501 --end 202608 --standard-json
sec macro us bank-credit --begin 202501 --end 202609 --standard-json
sec macro us lending-standards --begin 2025Q1 --end 2026Q3 --standard-json
sec macro us debt-service --begin 2025Q1 --end 2026Q2 --standard-json
sec macro us investment --component building-permits --begin 202501 --end 202608 --standard-json
sec macro us equipment-investment --price-basis nominal --begin 2025Q1 --end 2026Q2 --standard-json
sec macro us payrolls --begin 202501 --end 202609 --standard-json
sec macro us housing --begin 202501 --end 202609 --standard-json
sec macro us house-price --begin 2025Q1 --end 2026Q2 --standard-json
sec macro us trade --component goods --begin 202501 --end 202608 --standard-json
sec macro us real-trade --begin 2025Q1 --end 2026Q2 --standard-json
sec macro us target-rate --begin 20200301 --end 20200331 --standard-json
sec macro us debt --begin 20260930 --end 20260930 --standard-json
sec macro us debt-cash --begin 20260930 --end 20260930 --standard-json
sec macro us auctions --date-basis auction --begin 20260801 --end 20260831 --standard-json
sec macro cn pmi --begin 202607 --end 202609 --standard-json
sec macro cn nonmanufacturing-pmi --begin 202607 --end 202609 --standard-json
sec macro cn industrial --begin 202601 --end 202609 --standard-json
sec macro cn unemployment --age-groups --begin 202306 --end 202401 --standard-json
sec macro cn gdp --begin 2025Q1 --end 2026Q3 --standard-json
sec macro cn household --begin 2025Q1 --end 2026Q3 --standard-json
sec macro cn investment --sector infrastructure --begin 202509 --end 202609 --standard-json
sec macro cn enterprise --begin 202601 --end 202609 --standard-json
sec macro cn financing --begin 202601 --end 202609 --standard-json
sec macro cn loans --begin 202601 --end 202609 --standard-json
sec macro cn deposits --begin 202601 --end 202609 --standard-json
sec macro cn fiscal --budget general --begin 202501 --end 202609 --standard-json
sec macro cn trade --begin 202608 --end 202608 --standard-json
sec macro cn services-trade --begin 202501 --end 202608 --standard-json
sec macro cn lpr --begin 202501 --end 202512 --standard-json
sec macro cn reverse-repo --begin 20250101 --end 20251231 --standard-json
sec macro cn reverse-repo-history --begin 20240101 --end 20240719 --standard-json
sec macro cn rrr --begin 20200101 --end 20201231 --standard-json
sec macro cn treasury --begin 20260901 --end 20260930 --standard-json
```

各命令分别输出独立数据集。原有表格、别名、日期默认值、`--json`、`--refresh` 和 `--no-cache` 行为不变；`--json` 与 `--standard-json` 互斥。统一字段用于程序读取，不改变币种、单位或统计口径，也不自动生成中美对比。

## 契约版本 1

顶层包含 `schema_version="1"`、`region`、`begin/end`、`series`、`observations` 和 `original`。月度所属期使用 YYYYMM，季度使用 YYYYQ1 至 YYYYQ4，日度、周度和事件型使用 YYYYMMDD；每份数据集只包含一种频率，查询端点、观测日期按该频率校验。每个观测只包含一个值，通过 `series_id` 关联序列。消费者应检查版本、频率，并用序列 ID 和所属期作为联合键；观测带 `event_id` 时（如国债拍卖、存款准备金率），联合键还要加上 `event_id`。

`series` 为实际返回观测所引用的序列定义；查询窗口没有观测时，部分指标仍保留序列定义以说明口径，不代表有数据。每条定义包含：

| 字段 | 含义 |
| --- | --- |
| `id`、`indicator` | 序列标识、指标，例如 `us.m2.level`、`m2` |
| `measure` | `level`、`yoy_percent`、`mom_percent`、`ytd_level`、`ytd_yoy_percent`；GDP 另有 `qoq_percent`、`qoq_annualized_percent`；EFFR 为 `rate`、`previous_observation_change`；贷款标准和偿债比率为 `rate`、`yoy_percentage_points`、`qoq_percentage_points`；CN失业率为 `rate`；非农新增为 `monthly_change`；贸易差额变化为 `yoy_change`、`mom_change`、`qoq_change`；目标利率为 `lower_bound`、`upper_bound`、`lower_change`、`upper_change`；CN PMI为 `diffusion_index`；CN LPR、逆回购、国债收益率和准备金率的 `measure` 见各小节；CN GDP另有 `yoy_index`、`ytd_yoy_index`；CN居民收支增速为 `ytd_real_yoy_percent`；CN社融、贷款和存款的流量为 `monthly_flow`、`monthly_flow_derived`、`ytd_flow`，贷款另有 `reported_level`，存款另有 `reported_yoy_percent`；DTS官方金额为 `daily`、`month_to_date`、`fiscal_year_to_date`，国债和拍卖的其他 `measure` 见各小节 |
| `unit` | `CNY_100_million`（亿元人民币）、`USD_billion`（十亿美元）、`USD_million`（百万美元）、`square_meters_10000`（万平方米）、`percent`、`percentage_points`（百分点）、`basis_points`（基点）、`index`、`claims`（申请件数）、`persons`（人数）、`USD_per_hour`、`hours_per_week`、`housing_units_thousand`（千套）、`housing_units`（套）、`jobs_thousand`（千个岗位）、`USD`（美元面值）、`USD_per_100_par`（每100美元面值的价格）、`CNY_per_person`（元/人）、`USD_100_million`（亿美元） |
| `frequency` | `monthly`、`quarterly`、`daily`、`weekly`、`event`（只在实际发生时有观测） |
| `period_basis` | CN 货币为 `month-end`；US 货币为 `monthly-average`；社零单月、CPI、PCE、CN PMI、CN失业率和工业单月同比与环比为 `month`，社零、房地产和工业累计为 `year-to-date`；GDP 为 `quarter`，CN GDP累计和CN居民收支、固定资产投资为 `year-to-date`；CN工业企业利润为 `year-to-date`，余额为 `month-end`；CN社融、贷款和存款的余额为 `month-end`，单月流量为 `month`，累计为 `year-to-date`；CN财政累计和货物贸易累计为 `year-to-date`，贸易单月为 `month`；EFFR 为 `overnight-transaction-date`；目标利率、CN逆回购和准备金率为 `effective-date`；CN逆回购历史为 `publication-date`；CN国债收益率为 `valuation-date`；DTS日值为 `record-date`，累计为 `month-to-date`、`fiscal-year-to-date`；拍卖为 `issue-date` 或 `auction-date`；申领为 `week-ending-saturday`；银行信贷为 `pro_rata_average_of_weekly_levels`，贷款标准为 `survey_quarter`，偿债比率为 `calendar_quarter` |
| `seasonal_adjustment` | CN（PMI、工业环比和GDP环比除外）、EFFR 为 `not-seasonally-adjusted`；CN PMI、工业环比和GDP环比为 `seasonally-adjusted`；US M2、GDP、申领、PCE、实际消费／收入、工业指标、银行信贷、偿债比率、投资、非农、住宅销售、贸易为 `seasonally-adjusted`；贷款标准、房价指数、目标利率、国债交易与拍卖为 `not-seasonally-adjusted`；CPI各序列分别标记 |
| `price_basis` | 货币和社零、房地产金额及名义时薪为 `nominal`；实际GDP／消费／收入为 `real-chained`；CN工业增加值增速、CN GDP指数和增速、CN居民收支增速为 `real`（扣除价格因素）；CN GDP金额为 `constant-prices`（分段基期）；CN固定资产投资、工业企业、居民收支、社融、贷款、存款、财政和贸易金额为 `nominal`；CPI／PCE价格为 `price-index`；工业生产为 `volume-index`；房屋面积、PMI、CN失业率、利率（含CN LPR、逆回购、准备金率和国债收益率）、申领数量、产能利用率、劳动力比率、工时、贷款标准、偿债比率和住房套数为 `not_applicable`；银行信贷、名义设备投资和名义贸易为 `nominal`，实际设备投资和实际贸易为 `real-chained`；非农和住宅销售为 `not_applicable`，房价指数为 `price-index`；国债交易为 `nominal`，拍卖金额为 `par-value`，拍卖价格为 `not_applicable` |
| `annualized` | 水平与增速各自标记；US GDP／实际消费／实际收入／企业利润／住宅建设／设备投资／住宅销售／实际贸易水平、实际贸易差额变化和US GDP官方环比年化增速为 true，其余当前序列为 false |
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

### CPI：指数、季调和严格计算基期

`cpi` 与 `core-cpi` 各导出四条序列：`us.<indicator>.nsa.index`、`us.<indicator>.sa.index`、`us.<indicator>.nsa.yoy_percent`、`us.<indicator>.sa.mom_percent`。NSA同比、SA月环比不混用指数，后两者为未年化计算值。指数单位为 `index`，增速单位为 `percent`，`price_basis="price-index"`；可选 `index_base` 保留BLS指数基期，当前为 `1982-1984=100`，不把它当作单一年份。

`definition` 和 `source_series_id` 保留各自的BLS序列ID；每条序列明确自己的季调状态。官方指数保留原始数字字符串，计算增速的 `raw_value=null`；`base` 保留严格上年同月NSA指数或严格上一月SA指数。适配时校验当前／基期所属月份和来源序列，不用附近可用月份替代。NSA与SA的缺失状态分别保留，一条缺失不令另一条消失。

脚注、`latest` 标记、人口范围、批次和抓取时间均保留在 `original`。每条标准观测的抓取时间来自相应当前指数；若provider没有返回该指数点，`fetched_at=null`，批次抓取信息仍可从 `original.batches` 检查。`latest` 不是版本编号，也不是公布时间；不凭它填充修订信息。

### 失业金申领：周度所属期与四周窗口

初请、续请各导出 `us.<indicator>.count`、`weekly_change`、`four_week_average`。频率为 `weekly`、所属期口径为 `week-ending-saturday`；查询起止日期可为任意自然日，但每条观测必须是区间内的周六。初请单位为 `claims`，续请为 `persons`；两者均季调，`price_basis="not_applicable"`，不是全部失业人口。

周变化直接保留provider计算的当周减严格前周数量，单位与水平相同，不计算百分比。`base` 为严格7天前的日期、水平和原始字符串。四周均值保留当周及前三个连续日历周的完整输入，存于可选 `inputs` 数组，每项包含 `period/value/raw_value`。输入必须是四个正确的周六；任一数值缺失时保留空值，缺失原因 `window_incomplete`，不按剩余周数平均、不跨缺口寻找最近四条记录。

不前移续请所属周，也不因初请已有新值而补出续请值。最新可得周、各序列发布进度说明和完整provider结果保留在 `original`，缺失周不冒充零。原provider仍负责生成请求区间内的周六所属周；统一导出不额外补自然日。基期窗口可早于查询开始日，均来自同份已校验快照。

### PCE价格与实际消费：指数、金额和不可加性

PCE价格提供 `us.pce.index`、`us.core-pce.index` 及各自的 `yoy_percent`、`mom_percent`。指数基期为 `index_base="2017=100"`，单位为 `index`、季调、不折年，来源分别为PCEPI、PCEPILFE；这是链式价格指数，不是名义消费金额或实际消费量。同比和月环比均使用同一季调指数序列，属于未年化计算值，区别于CPI使用NSA指数计算同比。

实际消费提供 `us.real-pce.<component>.level`、`yoy_percent`、`mom_percent`，`component` 为 `total`、`goods`、`durable-goods`、`nondurable-goods` 或 `services`。水平单位为 `USD_billion`，`price_basis="real-chained"`、`reference_year=2017`、季调折年；不除以12冒充官方当月实际发生额。同比和月环比保留未年化计算值。

实际消费水平序列新增可选 `components_additive=false`，明确链式美元分项通常不可加；本次导出每个请求所选的官方序列，不相加耐用品与非耐用品生成商品，不相加商品与服务生成总量，也不计算分项贡献度。计算增速不带这个水平可加性字段；其他出口无此元数据时继续省略。序列ID同时包含指标和分项，价格指数、实际总量及各分项不会混为同一个联合键。

两类出口均保留官方水平的原始数字字符串、来源序列、抓取时间，以及计算增速的严格上一年同月／上一月基期。不重新计算或跨缺口换基期，零值和缺失分开处理；窗口外的基期仍来自同份快照。完整BEA口径、来源与原provider结果保存在 `original`，未提供的公布时间和修订编号保持 null。当前仍为可见修订版本，不是历史首次公布值。

### 实际收入与工业指标：总量、产出指数和利用率

实际收入提供 `us.real-income.level`、`yoy_percent`、`mom_percent`，直接保留DSPIC96实际可支配个人收入总量。水平为十亿美元、2017年链式美元、月度季调折年，属于扣除个人当期税及价格影响后的总量，不是人均收入或当月实际发生额。增速未年化，保留严格基期和原始水平。`coverage` 明确总体，BEA方法与来源信息另保留在 `original`；不自行用名义收入、人口或PCE指数重建序列。

工业生产与利用率均按 `total`、`manufacturing` 分开查询，序列ID包含部门，不相加或拼接。`coverage` 为版本1新增的可选字段，直接保留provider已核验的调查范围；其他出口没有该字段时继续省略。工业总量的美联储制造业范围包含伐木及部分出版业，制造业NAICS 31–33序列不包含两者，不能当作完全相同的总体。

- 生产指数：`us.industrial-production.<sector>.index`、`yoy_percent`、`mom_percent`。水平单位 `index`，`index_base="2017=100"`、`price_basis="volume-index"`，不是金额，水平不折年；增速为未年化计算值。来源为INDPRO或IPMAN。
- 利用率：`us.capacity-utilization.<sector>.rate`、`yoy_percentage_points`、`mom_percentage_points`。水平单位 `percent`、变化单位 `percentage_points`，公式为当期水平减基期水平；不是百分比增速，不设置指数基期或参考年。来源为TCU或MCUMFN。

两种变化分别选取provider的对应字段，适配层不重算。零基期不能用于百分比增速，但可用于百分点差；缺失基期均保留 null，不跨缺口替换。日期、原值、抓取时间及原provider结果完整保留。各系列的历史起点保存在原结果中，不因为共享快照而混用历史覆盖范围；未提供的公布时间和修订编号仍为 null。

### 劳动力比率与时薪工时：CPS和CES分别保留

失业率、劳动参与率分别提供 `us.<indicator>.rate`、`yoy_percentage_points`、`mom_percentage_points`。官方季调比率的单位为 `percent`，变化单位为 `percentage_points`，不计算相对百分比增速。序列新增可选 `survey`、`numerator`、`denominator`，直接保留provider的调查、分子和分母定义：失业率为失业人数／平民劳动力，劳动参与率为平民劳动力／非机构平民人口。`coverage` 保留16岁及以上非机构平民人口范围；人口控制脚注和可比性说明保存在 `original`，不把CPS住户调查与CES企业调查混用。

时薪、工时分别提供 `us.earnings.*`、`us.weekly-hours.*` 下的 `level`、`monthly_change`、`yoy_percent`、`mom_percent`。均为CES私人非农全部雇员（含全职、兼职）的月度季调观测，不包含政府雇员。平均时薪是名义税前值，单位 `USD_per_hour`；平均每周工时是平均支付工时，单位 `hours_per_week`，不是周度发布频率。月变化为相邻月水平差，与水平同单位；同／环比为未年化计算增速。水平和变化均不折年，不改成人均收入或劳动总时长。

四个出口均保留严格上月／上年同月的原始水平与来源序列。基期必须属于正确月份和同一来源序列；不从相邻输出行重建、不跨BLS批次拼算，直接保留provider同批计算结果。每条观测的 `fetched_at` 来自该当前点，不用一个批次时间覆盖全部月份。未返回当前点时可为 null，完整批次信息及当前／基期脚注、抓取时间均保留在 `original`。

CES官方水平新增可选 `preliminary`，表示原点是否标记P；false只表示未标P，不代表最终不再修订。没有返回原点时省略，不把未知状态写成false。计算差值／增速不设置该字段，其输入的初步标记可从 `original` 检查。此字段不改变原先各命令的 `--json`。公布时间和修订编号不可得时仍为 null，不从P或latest标记推算发布日期或版本编号。

### 美国财政：月度流量、赤字符号和财年累计

财政导出八条序列：`us.fiscal.receipts`、`outlays`、`surplus_or_deficit`、`deficit_positive`，以及对应的 `fytd_` 前缀序列。单位统一为 `USD_million`、名义、未季调、未折年，覆盖联邦统一预算，不是国债余额、债务发行或NIPA政府消费投资。

月度收入、支出、盈余／赤字为官方独立系列。`surplus_or_deficit` 为盈余正、赤字负；`deficit_positive` 对官方余额取反，正值为赤字、负值为盈余，并标记 `value_origin="derived"`。不以收入减支出覆盖官方余额；正赤字表示的 `inputs` 保留对应官方余额及原始字符串，转换值本身不伪造原始字符串。

财年累计为provider按当前快照内严格月份求和的计算值，全部标记 derived，不是另行采集的官方累计值。`period_basis="fiscal-year-to-date"`、`period_begin/period_end` 保留实际累计范围；新增可选 `fiscal_year` 保留所属财年。美国财年从前一年10月至当年9月，10月重新累计；适配时校验财年、财年月序、累计起止期及方法，不对累计值再次求和。各指标存在任一期缺失时，该累计值为 null、原因为 `window_incomplete`，不连带丢弃仍可用的当月值或其他指标。

累计数据的完整月度窗口输入未由provider结果返回，标准输出明确在 `note` 中说明，不从查询窗口内的几行重新构造累计。`original` 保留原累计值、方法与日期，而非完整HTTP响应或可独立复算全部累计的数据包。日期和来源仍可追溯到FRED全快照，原缓存策略保持不变。

### 企业部门：利润流量、月末库存和新订单

三个出口分别输出官方水平和严格同比、上期计算增速，不跨频率拼接或加总：

| 命令／序列前缀 | 水平口径 | 单位／频率 | 是否折年 |
| --- | --- | --- | --- |
| `corporate-profits`／`us.corporate-profits` | 含IVA、CCAdj的企业税前利润，含上市／非上市企业及来自世界其他地区的净利润 | `USD_billion`，季度 | 水平是SAAR；增速未年化 |
| `business-inventories`／`us.business-inventories` | 制造业、商人批发业、零售业的月末库存账面余额 | `USD_million`，月度 | 不折年 |
| `new-orders`／`us.new-orders` | 制造业当月新接订单扣除取消额 | `USD_million`，月度 | 不折年 |

均为名义、季调，利润不是扣除通胀的实际利润，库存不是库存投资流量，订单不是未完成订单余额。水平使用 `level`；同比为 `yoy_percent`，上期增速按频率使用 `qoq_percent` 或 `mom_percent`。严格基期缺失或非正时不计算增速，原因分别为 `baseline_missing`、`nonpositive_baseline`，不跳到附近可用值；负的当期利润保留，不将其冒充缺失。

`tax_basis="before_tax"` 为利润序列的可选元数据；`coverage` 保留官方定义。库存的可选 `as_of_date` 是provider按所属月生成的月末日期，不是实际公布日。NAICS范围重编、订单估计限制及完整来源信息保留在 `original`。这些可选字段不改变旧表格或原 `--json`；历史仍为当前可见修订值。

### 美国信用：月平均余额、调查净比例和偿债比率

三个命令各自输出独立数据集，均未折年，不合并成一个信用指标：

| 命令／序列前缀 | 水平口径 | 单位／频率 | 季调 | 变化序列 |
| --- | --- | --- | --- | --- |
| `bank-credit`／`us.bank-credit` | `LOANINV`，H.8商业银行信贷，周三余额按比例计算的月平均，不是月末余额或当月新增 | `USD_billion`，月度 | 是 | `yoy_percent`、`mom_percent` |
| `lending-standards`／`us.lending-standards` | `DRTSCILM`，SLOOS大中型企业C&I贷款标准净收紧比例，`survey="SLOOS"` | `percent`，季度 | 否 | `yoy_percentage_points`、`qoq_percentage_points` |
| `debt-service`／`us.debt-service` | `TDSP`，当前征信方法的家庭偿债比率 | `percent`，季度 | 是 | `yoy_percentage_points`、`qoq_percentage_points` |

水平序列分别使用 `level`（银行信贷）或 `rate`（两个比率）。银行信贷只接受百分比变化字段，两个比率只接受百分点变化字段，混入另一组单位时直接报错。可选 `methodology` 标记 `current_h8_monthly_levels`、`sloos_domestic_banks_large_middle_market_ci`、`credit_bureau_based_dsr`；旧方法的偿债比率档案不拼接。可选 `change_method` 只出现在变化序列上：银行信贷为 `level_ratio_not_break_adjusted`，表示由当前水平计算，不是H.8官方经断点调整的折年增速；两个比率为 `difference`，单位为百分点，公式为 `current_level - base_level`。

百分点差允许负值和零基期，净收紧比例为零是有效值，不产生 `zero_baseline`；银行信贷基期为零时才标记 `zero_baseline`。贷款标准的 `period` 沿用FRED调查季度，不回移到调查询问的回顾期。

### 美国投资：住宅建设套数和设备投资

| 命令与选项／序列前缀 | 水平口径 | 单位／频率 | 价格口径 |
| --- | --- | --- | --- |
| `investment --component housing-starts`／`us.investment.housing-starts` | `HOUST`，私人住宅新开工单元 | `housing_units_thousand`，月度 | `not_applicable` |
| `investment --component building-permits`／`us.investment.building-permits` | `PERMIT`，签发许可地区获准新建的私人住房单元 | `housing_units_thousand`，月度 | `not_applicable` |
| `equipment-investment --price-basis real`／`us.equipment-investment.real` | `Y033RX1Q020SBEA`，私人非住宅设备投资 | `USD_billion`，季度 | `real-chained`，`reference_year=2017` |
| `equipment-investment --price-basis nominal`／`us.equipment-investment.nominal` | `Y033RC1Q027SBEA`，私人非住宅设备投资 | `USD_billion`，季度 | `nominal` |

住宅序列的 `indicator` 分别为 `housing-starts`、`building-permits`，设备投资为 `equipment-investment`，按指标分组时不会混合。水平均为季调折年，`annualized=true`，不除以12或4换算为当期量；同比 `yoy_percent` 和上期增速 `mom_percent`／`qoq_percent` 未年化，`change_method="level_ratio"`。实际设备投资水平标记 `components_additive=false`，不与其他链式分项相加；增速序列不带此字段。

住房套数与设备金额的频率、单位不同，名义与实际设备投资的价格口径不同，均不共用序列ID、不拼接。严格基期缺失或为零时增速为空，原因分别为 `baseline_missing`、`zero_baseline`；零水平本身是有效值。信用和投资适配时，若provider在当期或严格基期不可用时仍给出计算值，直接报错，不输出 `available`。BEA账户代码、方法说明及完整来源保留在 `original`，历史仍为当前可见修订值，`published_at`、`revision_id` 为 null。

### 美国非农就业：岗位水平和相邻月差额

`payrolls` 输出 `us.payrolls.level`（`CES0000000001`，官方水平）和 `us.payrolls.monthly_change`（计算值），单位都是 `jobs_thousand`，季调、未折年。它按岗位计数，不是去重后的就业人数。

新增等于当月水平减严格相邻上月水平，零新增是有效值，不跨缺口；上月水平缺失时为 `baseline_missing`。官方水平保留可选 `preliminary`，含义与时薪工时相同，未标P不代表最终值。长历史分批时，一月的基期可能与前一行十二月的展示值来自不同批次，`base` 保留实际使用的值。

某月没有返回原点时，`fetched_at` 使用展示该月批次的抓取时间。本机观测版本只保留在 `original`，它们不是官方首次、第二次、第三次估计。`published_at`、`revision_id` 为 null。

### 美国住宅：销售和房价指数

| 命令与选项／序列前缀 | 水平口径 | 单位／频率 | 季调与折年 |
| --- | --- | --- | --- |
| `housing --component new-sales`／`us.housing.new-sales` | `HSN1F`，新建单户住宅签约或收取定金 | `housing_units_thousand`，月度 | 季调折年 |
| `housing --component existing-sales`／`us.housing.existing-sales` | `EXHOSLUSM495S`，既有住宅已完成交易 | `housing_units`，月度 | 季调折年 |
| `house-price`／`us.house-price` | `USSTHPI`，FHFA全交易重复交易指数，`index_base="1980Q1=100"`，序列 `measure` 为 `index` | `index`，季度 | 未季调、未折年 |

默认 `housing` 的 `--component all` 在同一数据集中输出新房和成屋两组序列，`indicator` 分别为 `new-sales`、`existing-sales`。两者单位相差1000倍、成交阶段不同，不换算、不加总。折年水平不除以12。增速 `yoy_percent`、`mom_percent`、`qoq_percent` 未年化，`change_method="level_ratio"`；严格基期缺失或为零时为空。

成屋只有当前FRED公开快照可见的短窗口，窗口外为 `source_value_missing`，`note` 说明超出覆盖范围，不补历史。房价的季环比未季调由序列的 `seasonal_adjustment` 表明，不写入 `note`。住宅和贸易的 `note` 按各值的 `missing_reason` 逐值生成，不混入同一行其他序列的说明；当期缺失时保留provider的说明，例如成屋覆盖范围。完整逐行备注保留在 `original`。

### 美国贸易：出口、进口和官方差额

`trade`（月度名义、国际收支口径）和 `real-trade`（季度实际、NIPA口径）分别输出，不拼接。序列ID为 `us.<indicator>.<component>.<measure>`，`component` 为 `total`、`goods`、`services`：

| 命令 | `indicator` | 单位 | 价格、折年 | `methodology` |
| --- | --- | --- | --- | --- |
| `trade` | `exports`、`imports`、`trade-balance` | `USD_million` | `nominal`，未折年 | `bop` |
| `real-trade` | `real-exports`、`real-imports`、`real-net-exports` | `USD_billion` | `real-chained`，`reference_year=2017`，水平折年 | `nipa` |

出口和进口的变化为未年化百分比，`change_method="level_ratio"`。差额可为负或零，所以不计算百分比：变化为 `yoy_change` 和 `mom_change` 或 `qoq_change`，单位与水平相同，`change_method="difference"`，允许零及负基期。实际贸易的差额变化是折年水平之差，因此 `annualized=true`，与未年化的增速不同。差额水平直接保留官方值，不以出口减进口替代。

实际贸易的水平序列标记 `components_additive=false`。商品和服务的官方实际净额未采集，`real-trade --component goods|services` 不输出 `real-net-exports` 序列，也不用分项相减。

### 美国联邦基金目标区间：调整事件

`target-rate` 的 `frequency="event"`，`period` 是官方生效日（YYYYMMDD，`period_basis="effective-date"`）。观测只包含查询窗口内的实际调整，不为每日、周末或维持利率的会议补行；窗口内没有调整时 `observations` 为空。

| 序列 | 单位 | 口径 |
| --- | --- | --- |
| `us.target-rate.lower_bound`、`upper_bound` | `percent` | 官方调整表的区间上下限，`raw_value` 都保留区间原文，如 `1.00-1.25` |
| `us.target-rate.lower_change`、`upper_change` | `basis_points` | 上下限分别相对上一次实际调整的差值，标记 `derived` |

变化由上下限相减得到，provider已校验它与官方调整基点一致，因此 `change_method="difference_checked_against_official_adjustment"`。`base.period` 是上一次调整的生效日，可以在窗口外；两个变化的 `base` 分别保留上限或下限。相邻变化的前次生效日或数值不一致，或非起点事件缺少变化值时，直接报错。区间前基准和窗口内事件都校验日期、上下限、来源、抓取时间和公布信息；公布日期须为YYYY-MM-DD且不晚于生效日，`published_at` 须与其同一天。

2008-12-16 由单一目标切换为区间，没有同口径前值：变化为 null，原因为 `methodology_break`，`base` 为 null；官方 `75-100` 基点原文保留在 `original`。下限 0 和所有负变化都是有效值。

公布信息与生效日分开：`published_date` 是通过核验的声明日期（YYYY-MM-DD），`published_at` 是具体公布时间，并保留官方时区。声明未提供时间（`date-only`）或页面不存在、内容无法匹配（`unavailable`）时，相应字段为 null，并在该事件的所有观测的 `note` 中说明原因，这些说明不代表数值缺失。声明或日历页暂时无法下载（网络错误、404和410以外的HTTP错误）时，`--standard-json` 直接报错，不输出部分结果；普通表格和 `--json` 仍显示调整表数据，`--json` 用 `release_fetch_failed=true` 标记该事件。不把生效日减一天，也不把会议日期当作公布日期。

区间前基准 `baseline` 和截至查询结束日仍有效的区间 `current` 不是窗口内事件，只保留在 `original`。查询“某日的有效区间”应读取 `original.current`；新增规范观测会把它们混入调整事件。声明链接和原文同样在 `original`。历史为当前可见版本，不提供当时可得版本的回测保证。

### 美国国债记账发行与赎回

`debt` 的 `frequency="daily"`，`period` 是DTS记录日，周末、假日和未发布日不补行。金额单位都是 `USD_million`，可以为负，不取绝对值。

- **官方明细**：`us.treasury-debt.<market>.<transaction>.<label>.<measure>`，`market` 为 `marketable` 或 `nonmarketable`，`transaction` 为 `issues` 或 `redemptions`，`label` 由官方类别和细项生成，原文在 `definition`。每行输出 `daily`、`month_to_date`、`fiscal_year_to_date` 三条观测，都是官方值，累计值不由日值重算。
- **计算汇总**：`us.treasury-debt.<market>.issues_total`、`redemptions_total`、`net_recorded_issuance`，标记 `derived`。任一输入缺失时为 null，原因为 `calculation_unavailable`。汇总的输入行数和完整明细在 `original`。

累计观测用 `period_begin`、`period_end`（YYYYMMDD）标明范围：月累计从当月1日开始，财年累计从10月1日开始，`fiscal_year` 保留所属财年。这个口径是记账值，含更正、利息和通胀调整，不等于现金流、拍卖面值或公众净融资。上游偶有同日内容完全相同的重复行（2007-02-06的非流通Domestic、Foreign Series），按一条输出并在 `note` 中列出全部源行号，完整行保留在 `original`，也不重复计入汇总；同一标签的金额不同时报错。`debt-cash` 的调整明细采用同样规则。

### 美国国债现金发行与赎回

`debt-cash` 同样按DTS记录日输出，单位 `USD_million`，每项都有 `daily`、`month_to_date`、`fiscal_year_to_date`：

| 序列 | 来源 |
| --- | --- |
| `us.treasury-debt-cash.issues.*`、`redemptions.*` | DTS表II转引IIIB的官方现金总额 |
| `us.treasury-debt-cash.net.*` | 同一记录日、同一字段的官方发行减赎回，标记 `derived`，`inputs` 保留两侧数值及原文 |
| `us.treasury-debt-cash.adjustment.<transaction>.<label>.*` | IIIB记账转现金明细，只作溯源 |

某一侧总额未列示时，该侧观测为 `source_value_missing`，净额为 `calculation_unavailable`，不用调整项推算。调整明细不相加重建总额。`(-)` 是官方方向标签，数值不翻转符号；序列ID中写作 `minus`。

### DTS标签与序列ID

DTS的类别和细项在不同时期写法不同（如 `Table IIIA` 和 `Table III-A`，`Bills (-)` 在类别或细项列）。序列ID按原文生成，不做归并，所以写法不同的历史行属于不同序列。需要跨期比较时应查看 `definition`，自行确认口径。标点不同的两个标签生成同一ID时直接报错，不合并。

### 美国国债拍卖：逐券事件

`auctions` 的 `frequency="event"`，`period` 按 `--date-basis` 取发行日或拍卖日，由 `period_basis` 标明。同一天有多只证券，所以新增两个可选字段，其他导出不受影响：

- 观测的 `event_id`，格式为 `CUSIP/发行日/拍卖日`，不随日期口径变化。联合键是 `series_id`、`period` 和 `event_id`。
- 顶层 `events` 每个事件一条，保留CUSIP、证券类型、期限、公告日、拍卖日、发行日、到期日、原发行日，以及再发行、浮息、通胀保值和SOMA／FIMA包含关系标记。未知标记为 null，不写成 false。

每个事件输出十条观测：`offering`、`total_accepted`、`competitive_accepted`、`noncompetitive_accepted`、`soma_accepted`、`fima_noncompetitive_accepted`、`primary_dealer_accepted`、`direct_bidder_accepted`、`indirect_bidder_accepted`，单位 `USD`，是面值，不是现金收入；以及 `price_per_100`。金额序列标记 `components_additive=false`，各分项可能交叠，不要相加重建发行额。利率、收益率等其他官方字段保留在 `original`。

`debt`、`debt-cash`、`auctions` 的历史都是当前可见版本，`published_at`、`revision_id` 为 null。

### 中国PMI：季调扩散指数

`pmi` 输出 `cn.pmi.<indicator>.diffusion_index`，`nonmanufacturing-pmi` 输出 `cn.nonmanufacturing-pmi.<indicator>.diffusion_index`。两个调查都有 `new-orders` 等同名分项，序列和 `indicator` 都带调查前缀，不混用。

单位是 `index`，季调，50为临界点。官方以百分数发布（原值保留在 `original`），但它是扩散指数水平，不减100，也不是产出增速，所以不用 `percent`。投入品价格、销售价格和供应商配送时间的 `coverage` 另外说明读法：高于50分别表示价格上升和交货加快，不能统一解释为景气改善。`source_series_id` 是发布库指标ID，`definition` 保留官方名称。`--details` 只影响表格，统一导出总是包含非制造业全部12项。

### 中国工业增加值：同比、累计和环比

`industrial` 输出三条序列，单位都是 `percent`，都是扣除价格因素的实际增速：

| 序列 | 含义 | 季调 | 来源 |
| --- | --- | --- | --- |
| `cn.industrial.yoy_percent` | 单月同比 | 否 | 发布库 |
| `cn.industrial.ytd_yoy_percent` | 年初累计同比，`period_begin`、`period_end` 标明累计范围 | 否 | 发布库 |
| `cn.industrial.mom_percent` | 季调环比 | 是 | 官方发布稿，有自己的 `source`、`fetched_at`、`published_at` |

1—2月合并发布时，2月单月同比为 null，累计值不复制成单月，也不从累计增速相减得到单月。`published_at` 只出现在环比上，来自发布列表，不套用到发布库数据。`note` 按指标分开：1—2月说明在同比和累计上，环比未检索到的说明只在环比上。1月同比或累计为 null 时说明官方通常合并发布1—2月，只表示接口未返回，不据此断言每年的原因。发布稿缺失不代表当月从未发布环比。

### 中国城镇调查失业率：口径版本

`unemployment` 输出 `cn.unemployment.<indicator>.<definition_version>.rate`，单位 `percent`，未季调，是失业率水平。默认只有全国值，`--age-groups` 再加四个年龄组。

16—24岁的同一个官方指标跨越2023年口径调整，所以按 `definition_version` 拆成不同序列，不跨断点连接；`indicator` 相同，都是 `unemployment.age-16-24`。`methodology` 保留口径版本和在校生范围。2023年7—11月暂停发布期，16—24岁和旧25—59岁的空值原因都是 `publication_paused`（16—24岁单列暂停期序列，旧25—59岁沿用原序列）；新分组此时尚未开始发布，是 `source_value_missing`。`note` 说明该月适用的口径，所以有值的观测也会带。

### 中国GDP：金额、指数和增速

`gdp` 输出七条季度序列：

| 序列 | 含义 | 来源 |
| --- | --- | --- |
| `cn.gdp.level`、`ytd_level` | 不变价当季、累计金额，亿元 | 官方 |
| `cn.gdp.yoy_index`、`ytd_yoy_index` | 上年同期=100指数，`index_base` 标明 | 官方 |
| `cn.gdp.yoy_percent`、`ytd_yoy_percent` | 指数减100，`inputs` 保留官方指数 | 计算 |
| `cn.gdp.qoq_percent` | 季调环比，未年化 | 官方 |

金额按不同基期分段，`reference_year` 不设置，序列的 `coverage` 保留官方注释，不能跨基期直接比较。累计观测用 `period_begin`、`period_end` 标明范围。发布库不提供逐值原文，`raw_value` 为 null。

### 中国居民收支：累计人均值

`household` 输出 `cn.household.income.*` 和 `cn.household.consumption.*`，`ytd_level` 为现价元/人（`CNY_per_person`），`ytd_real_yoy_percent` 为官方累计实际同比。都是年初至期末累计，未季调、未折年。`raw_value` 保留发布库原文。全年累计含自产自用，季度累计不含，所以Q4的观测带 `note`，不要相减得到第四季度。

### 中国固定资产投资：累计值和统计范围

`investment` 输出 `cn.investment.<sector>.ytd_level` 和 `ytd_yoy_percent`，都是年初累计、名义、未季调。基建的统计范围有断点，序列ID带上范围（`excluding_electricity_heat_gas_water` 或 `including_electricity_heat_gas_water`），不跨范围连接；`indicator` 相同。1月免报和未检索到月报的月份无法确定范围，单独成 `cn.investment.infrastructure.ytd_level` 和 `ytd_yoy_percent`，只有缺失观测，不把它们归入任一范围。

1月免报时原因是 `january_exempt`，未检索到月报时是 `report_not_found`，这两种情况的 `source`、`fetched_at` 都是 null。基建等分项未公布金额时，金额观测为 `source_value_missing` 并说明不反推，增速观测的 `note` 保留当期可比口径说明。`published_at` 取稿件页面时间。

### 中国工业企业：累计利润和月末余额

`enterprise` 输出 `cn.enterprise.<metric>.<scope_standard>[.<accounting_scope>].<measure>`。调查范围和应收账款列报方式变化时序列分开，不跨断点连接；`coverage` 保留调查范围。

| 指标 | `measure` | 含义 |
| --- | --- | --- |
| `profit` | `ytd_level`、`ytd_yoy_percent` | 年初累计流量，可以为负；2月是1—2月合并，不是单月 |
| `inventory`、`finished-goods`、`receivables` | `level`、`yoy_percent` | 月末余额，`as_of_date` 是月末日期 |

缺失原因区分 `january_exempt`、`not_surveyed`（2007—2010年只调查2、5、8、11月，含这些年的1月）、`definition_suspended`（2019年应收账款）和 `official_not_computable`（官方标注利润同比不可计算），其余为 `source_value_missing`。`note` 只出现在缺失观测上，同比都是官方值，不自行计算。

### 中国社会融资规模：存量、增量和计算累计

`financing` 对总量和每个融资分项分别输出 `level`（月末存量）、`yoy_percent`（官方存量同比）、`monthly_flow`（官方当月增量）和 `ytd_flow`（年初累计）。单位统一为亿元；官方存量表原单位万亿元，换算后的值和 `stock_source_unit` 在 `original`，换算前的原文只在provider缓存里。金额序列标记 `components_additive=false`。

统计范围变化的分项按官方日期分序列，序列ID为 `cn.financing.<key>[.<version>].<measure>`，`version` 形如 `before-201909`、`since-201909`，不跨断点连接；范围未变的分项不带版本。总量在2018-07、2018-09、2019-09、2019-12、2023-01分段，企业债券在2019-09，人民币贷款和贷款核销在2023-01。未识别的新渠道生成 `other:<原表名称>` 键，待核验，ID不保证稳定。

`ytd_flow` 是同一年度表从1月起逐月求和的计算值，标记 `derived`，不是官方累计发布值；更早月份缺失时为 null，原因是 `window_incomplete`；当月增量本身缺失时是 `source_value_missing`。存量和增量分别保留各自的 `source`、`fetched_at`。分项不假定相加等于总量；历史“地方政府专项债券”和较新的“政府债券”键不同，是不同序列。社融人民币贷款与金融机构人民币贷款是不同口径。

### 中国人民币贷款：余额、报告值和新增

`loans` 对总额和七个分项输出 `cn.loans.<key>.<version>.level`（信贷收支表精确余额）、`monthly_flow` 或 `monthly_flow_derived`、`ytd_flow`。`version` 是 `before-202301` 或 `since-202301`：2023年1月消费金融、理财和金融资产投资公司纳入金融统计范围，前后不共用序列。金额序列标记 `components_additive=false`。总额另有 `reported_level`（报告四舍五入余额）和 `yoy_percent`（官方同比）。余额来自年度表，其他来自金融统计报告，各自保留来源，报告值带 `published_date`。

报告直接公布月度新增（或1月累计）时进入官方序列 `monthly_flow`。只有相邻报告累计值时，相减结果进入 `monthly_flow_derived`，标记 `derived`，`note` 说明是舍入后的近似值并给出上期报告，`inputs` 保留上月和当月累计；同一月只出现在其中一条序列。两者都没有时 `monthly_flow` 为 null，原因是 `calculation_unavailable`；未检索到报告时为 `report_not_found`，余额仍可用。余额表尚未更新而报告已发布时，该月余额为 null，报告值照常输出；没有报告的空模板月份不输出。不以余额差额代替新增。

### 中国住户存款：余额、同比和新增

`deposits` 输出 `cn.deposits.households.<version>.level`、`cn.deposits.demand.<version>.level`、`cn.deposits.time-and-other.<version>.level`（月末余额，`raw_value` 保留表内原文，`version` 同贷款），以及住户的 `monthly_flow` 或 `monthly_flow_derived`、`ytd_flow`，规则同贷款。

同比有两条：`yoy_percent` 由当前年度表严格上年同月余额计算，标记 `derived`，`change_method="level_ratio_not_comparable_scope"`，未调整统计范围，不是官方可比同比；2023年全年基期在纳入新机构之前，跨统计范围为 null，原因是 `methodology_break`，provider的计算值保留在 `original`；`reported_yoy_percent` 只接受报告明确公布的住户存款同比。目前报告没有这一项，所以它是 null 并说明全部人民币存款同比不套用。

### 中国财政收支：累计金额和同比

`fiscal` 输出 `cn.fiscal.<key>.ytd_level`（亿元）和 `ytd_yoy_percent`，`key` 是 `general-revenue`、`general-expenditure`、`fund-revenue`、`fund-expenditure`，`--budget` 决定输出哪些。都是当年累计名义值，未季调、未年化。官方另列同口径或扣除留抵退税的同比时，才有 `ytd_comparable_yoy_percent` 和 `ytd_tax_refund_adjusted_yoy_percent`；没提到不算缺失，所以这两条序列可能不出现。

目录未收录的月份原因是 `report_not_found`，`note` 保留说明，例如1月通常并入1—2月合并发布。官方只说“基本持平”而没给百分比时，同比为 null，`note` 说明。两本预算不相加，不计算赤字，不由累计相减得到当月。

### 中国货物贸易：双币种和主要伙伴

`trade` 输出 `cn.goods-trade.<cny|usd>.<flow>.<measure>`，`flow` 是 `total`、`exports`、`imports`、`balance`，单位分别是 `CNY_100_million`、`USD_100_million`。两种币种是独立的官方统计，不互相换算；同一 `indicator` 下要按序列ID区分币种。`measure` 是 `level`（当月）、`ytd_level`、`yoy_percent`、`ytd_yoy_percent`，人民币另有 `mom_percent`。某币种没有当月记录时原因是 `report_not_found`，另一币种不受影响。

主要伙伴输出 `cn.goods-trade.partner.<cny|usd>.<官方行名>.<flow>.<measure>`，只有查询截至月，币种由 `--partners-currency` 决定。序列ID保留中文行名，行名变化时序列也会变化。总值、区域组织和成员行范围交叠，金额序列标记 `components_additive=false`。美元伙伴只有累计值，不差分出当月；人民币伙伴另有 `level`。

### 中国服务贸易：双币种月值

`services-trade` 输出 `cn.services-trade.<cny|usd>.<flow>.level`，`flow` 同上。总额和差额直接取官方字段，有独立舍入，不用出口加减进口覆盖。该月没有记录时原因是 `report_not_found`，不拆分合并期。网站记录时间不当作公布时间，`published_at` 为 null。

### 中国LPR：月度报价和相邻月变化

`lpr` 输出 `cn.lpr.1y.rate`、`cn.lpr.5y-plus.rate`（官方报价，`percent`）和 `cn.lpr.1y.change`、`cn.lpr.5y-plus.change`（`basis_points`，标记 `derived`）。`period` 是报价月，`as_of_date` 是公告里的实际报价日（YYYYMMDD），不固定为20日。公布时间精确到秒时写入 `published_at`，只有日期时只写 `published_date`。

变化只比较严格相邻上月，持平是有效的0。上月报价在查询窗口内时 `base` 带值；在窗口外时 `base.value` 为 null 且不反推，变化值仍可用。未检索到当月公告时是 `report_not_found`；缺月后的下一月是 `baseline_missing`，不跨缺口；2019年8月改革首月是 `methodology_break`。

### 中国七天逆回购：调整公告和中标历史

`reverse-repo` 是事件型，`period` 是生效日，只输出窗口内的调整公告：`cn.reverse-repo.7d.rate`（`percent`）和 `cn.reverse-repo.7d.change`（`basis_points`，标记 `derived`），`base.value` 是公告原文的调整前利率。窗口内第一条的 `base.period` 来自区间前最后一次公告；2024-07-22 是覆盖起点，前次生效日未知，`base.period` 为空。窗口内没有调整时 `observations` 为空，某日的有效利率要读 `original.previous_event`，和美国目标利率的处理相同。

`reverse-repo-history` 是固定利率机制前逐次操作的中标利率：`cn.reverse-repo-history.7d.awarded_rate`，`period` 是公告公布日。同一天可有多次操作，所以观测带 `event_id`。正文写明操作日时写入 `as_of_date`，否则为空，不推断。`--limit` 只影响表格和普通 `--json`，统一导出总是包含请求窗口内的全部操作。这是逐次操作结果，不是政策利率调整事件，也不和 `reverse-repo` 连接。

### 中国存款准备金率：调整幅度和机构范围

`rrr` 输出 `cn.rrr.change_lower`、`cn.rrr.change_upper`（`percentage_points`，负值为下调），是官方调整幅度，固定幅度时上下界相等，区间不取均值，也不推算准备金率水平。`period` 是生效日。同一生效日可能有多个机构范围，所以观测带 `event_id`，格式为 `生效日/范围类型/内容哈希`。

顶层可选字段 `policy_events` 每个事件一条，保留适用范围、豁免、是否额外降准、分批序号和公告标题。它和国债拍卖的 `events` 字段内容不同，所以分开；其他导出都不带这个字段。

### 中国国债收益率：全部关键期限

`treasury` 输出 `cn.treasury.<tenor>.yield`，`tenor` 是 `3m`、`6m`、`1y`、`2y`、`3y`、`5y`、`7y`、`10y`、`15y`、`20y`、`30y`，单位 `percent`，`period` 是估值日。表格只显示六个期限，统一导出保留全部十一个，`raw_value` 保留接口原文。空值为 `source_value_missing`，无记录的日期不补行。接口没有公布时间，命令也不缓存，所以 `fetched_at`、`published_at` 为 null。单次查询区间须小于一年，这是接口限制。

## 缺失与溯源

每条观测保留 `period`（按频率使用YYYYMM、YYYYQn或YYYYMMDD）、`value`、`status`、`missing_reason`、`raw_value`、`source`、`fetched_at`、`published_date`、`published_at`、`revision_id`、`data_version`、`base`、`note`。`index_base`、`inputs`是版本1的可选扩展字段，原出口无此元数据时仍省略，不修改原JSON结构。

- 数值存在时 `status="available"`，包括零值和负增速；`missing_reason=null`。
- 数值缺失时 `value=null`、`status="missing"`。`report_not_found` 表示人民银行当前目录未检索到月份；`source_value_missing` 表示源水平或报告字段缺失；计算增速另外区分 `current_value_missing`、`baseline_missing`、`zero_baseline`，其他无法计算的情况为 `calculation_unavailable`。CN失业率年龄分组暂停发布期为 `publication_paused`；CN投资和工业企业的其他原因见各小节。缺失不自动归因于未发布、停更或历史不存在。
- 含 `base` 的计算值保留指标规定的严格基期，见各指标小节；四周均值使用完整 `inputs`。官方水平和直接发布的官方增速的 `base=null`。不重新计算、不跨缺口寻找替代基期。
- FRED／BLS已提供的官方原值保留源数字字符串，包括缺失标记；计算值的原始字符串为 null。CN居民收支、住户存款、货物贸易、服务贸易、逆回购历史和国债收益率保留原文；其他CN provider 未提供逐字段原始数字字符串，该字段为 null，不把格式化后的数值冒充原值。NBS逐字段来源标识另存为 `source_series_id`。EFFR的原始JSON记录保留在 `original`，规则见前节。
- US货币、CPI、申领、劳动力比率、时薪工时、PCE与实际消费／收入、工业、企业、信用、投资、非农、住宅和贸易的 `note` 只出现在缺失观测上。住宅和贸易按各值的缺失原因生成；其余指标说明该行的缺失原因，同一行多个变化值都缺失时共用同一说明，逐值原因以 `missing_reason` 为准。完整逐行备注保留在 `original`。
- `source` 和 `fetched_at` 保留 provider 实际来源与抓取时刻，命中缓存时保留原抓取时间。未找到 CN 报告时两者均为 null，不输出公元 1 年的虚假时间。
- CN `published_date` 保留报告原有发布日期字段，仅表示源日期及其原精度；不填充零点作为精确公布时间。无法取得的 `published_at`、`revision_id` 为 null。
- `data_version="current_visible_revision"` 表示查询时当前可见历史；不能据此进行“当时已知”的历史回测，也不提供完整 vintage 档案。US 原 provider 的版本标签另外保存在 `original` 中。

`original` 是适配前的完整 provider JSON 结果，保留原统计注释、方法变化、基期和来源字段。它**不是上游 HTTP 原始响应**；原始 HTML/CSV 的缓存策略仍由 provider 管理。新增适配不会改写原 provider 结果。

网络、解析、编码失败直接报错，不输出部分成功的数据集；状态字段不用于把请求失败伪装为数据缺失。当前不增加数据过期判定，沿用 provider 六小时缓存与失败处理；`available` 仅表示该观测有数值，不承诺它是最新公布期。

## 覆盖与后续迁移

`macro us` 和除价格外的 `macro cn` 命令都已接入，各自口径见上文；`macro cn cpi`、`core-cpi`、`ppi` 尚未接入，继续使用原 JSON。TODO 中全局基础项仍保留未完成状态。其他指标、部分抓取失败、完整历史版本与中美可比性需要各自的适配和测试，不因本批格式扩展而视为已完成。

测试覆盖真实离线样本、旧输出回归、官方值／计算值、M1口径边界、房地产范围变化、累计所属期、1—2月合并、零与缺失、严格基期、空结果、元数据空值、重复月份、非法范围、非有限值、上游错误、取消及输出写入失败。
