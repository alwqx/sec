# 宏观数据统一导出

`--standard-json` 提供版本化的宏观序列 JSON。已接入指标及各自口径见下文，包括货币、消费、就业、生产、财政、企业部门、信用、投资、住宅和贸易。采集仍使用原provider及缓存，不增加网络请求。

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
```

各命令分别输出独立数据集。原有表格、别名、日期默认值、`--json`、`--refresh` 和 `--no-cache` 行为不变；`--json` 与 `--standard-json` 互斥。统一字段用于程序读取，不改变币种、单位或统计口径，也不自动生成中美对比。

## 契约版本 1

顶层包含 `schema_version="1"`、`region`、`begin/end`、`series`、`observations` 和 `original`。月度所属期使用 YYYYMM，季度使用 YYYYQ1 至 YYYYQ4，日度、周度和事件型使用 YYYYMMDD；每份数据集只包含一种频率，查询端点、观测日期按该频率校验。每个观测只包含一个值，通过 `series_id` 关联序列。消费者应检查版本、频率，并用序列 ID 和所属期作为联合键。

`series` 为实际返回观测所引用的序列定义；查询窗口没有观测时，部分指标仍保留序列定义以说明口径，不代表有数据。每条定义包含：

| 字段 | 含义 |
| --- | --- |
| `id`、`indicator` | 序列标识、指标，例如 `us.m2.level`、`m2` |
| `measure` | `level`、`yoy_percent`、`mom_percent`、`ytd_level`、`ytd_yoy_percent`；GDP 另有 `qoq_percent`、`qoq_annualized_percent`；EFFR 为 `rate`、`previous_observation_change`；贷款标准和偿债比率为 `rate`、`yoy_percentage_points`、`qoq_percentage_points`；非农新增为 `monthly_change`；贸易差额变化为 `yoy_change`、`mom_change`、`qoq_change`；目标利率为 `lower_bound`、`upper_bound`、`lower_change`、`upper_change` |
| `unit` | `CNY_100_million`（亿元人民币）、`USD_billion`（十亿美元）、`USD_million`（百万美元）、`square_meters_10000`（万平方米）、`percent`、`percentage_points`（百分点）、`basis_points`（基点）、`index`、`claims`（申请件数）、`persons`（人数）、`USD_per_hour`、`hours_per_week`、`housing_units_thousand`（千套）、`housing_units`（套）、`jobs_thousand`（千个岗位） |
| `frequency` | `monthly`、`quarterly`、`daily`、`weekly`、`event`（只在实际发生时有观测） |
| `period_basis` | CN 货币为 `month-end`；US 货币为 `monthly-average`；社零单月、CPI与PCE为 `month`，社零和房地产累计为 `year-to-date`；GDP 为 `quarter`；EFFR 为 `overnight-transaction-date`；目标利率为 `effective-date`；申领为 `week-ending-saturday`；银行信贷为 `pro_rata_average_of_weekly_levels`，贷款标准为 `survey_quarter`，偿债比率为 `calendar_quarter` |
| `seasonal_adjustment` | CN、EFFR 为 `not-seasonally-adjusted`；US M2、GDP、申领、PCE、实际消费／收入、工业指标、银行信贷、偿债比率、投资、非农、住宅销售、贸易为 `seasonally-adjusted`；贷款标准、房价指数、目标利率为 `not-seasonally-adjusted`；CPI各序列分别标记 |
| `price_basis` | 货币和社零、房地产金额及名义时薪为 `nominal`；实际GDP／消费／收入为 `real-chained`；CPI／PCE价格为 `price-index`；工业生产为 `volume-index`；房屋面积、利率、申领数量、产能利用率、劳动力比率、工时、贷款标准、偿债比率和住房套数为 `not_applicable`；银行信贷、名义设备投资和名义贸易为 `nominal`，实际设备投资和实际贸易为 `real-chained`；非农和住宅销售为 `not_applicable`，房价指数为 `price-index` |
| `annualized` | 水平与增速各自标记；GDP／实际消费／实际收入／企业利润／住宅建设／设备投资／住宅销售／实际贸易水平、实际贸易差额变化和GDP官方环比年化增速为 true，其余当前序列为 false |
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

## 缺失与溯源

每条观测保留 `period`（按频率使用YYYYMM、YYYYQn或YYYYMMDD）、`value`、`status`、`missing_reason`、`raw_value`、`source`、`fetched_at`、`published_date`、`published_at`、`revision_id`、`data_version`、`base`、`note`。`index_base`、`inputs`是版本1的可选扩展字段，原出口无此元数据时仍省略，不修改原JSON结构。

- 数值存在时 `status="available"`，包括零值和负增速；`missing_reason=null`。
- 数值缺失时 `value=null`、`status="missing"`。`report_not_found` 表示人民银行当前目录未检索到月份；`source_value_missing` 表示源水平或报告字段缺失；计算增速另外区分 `current_value_missing`、`baseline_missing`、`zero_baseline`，其他无法计算的情况为 `calculation_unavailable`。缺失不自动归因于未发布、停更或历史不存在。
- 含 `base` 的计算值保留指标规定的严格基期，见各指标小节；四周均值使用完整 `inputs`。官方水平和直接发布的官方增速的 `base=null`。不重新计算、不跨缺口寻找替代基期。
- FRED／BLS已提供的官方原值保留源数字字符串，包括缺失标记；计算值的原始字符串为 null。当前接入的 CN provider 未提供逐字段原始数字字符串，该字段为 null，不把格式化后的数值冒充原值。NBS逐字段来源标识另存为 `source_series_id`。EFFR的原始JSON记录保留在 `original`，规则见前节。
- US货币、CPI、申领、劳动力比率、时薪工时、PCE与实际消费／收入、工业、企业、信用、投资、非农、住宅和贸易的 `note` 只出现在缺失观测上。住宅和贸易按各值的缺失原因生成；其余指标说明该行的缺失原因，同一行多个变化值都缺失时共用同一说明，逐值原因以 `missing_reason` 为准。完整逐行备注保留在 `original`。
- `source` 和 `fetched_at` 保留 provider 实际来源与抓取时刻，命中缓存时保留原抓取时间。未找到 CN 报告时两者均为 null，不输出公元 1 年的虚假时间。
- CN `published_date` 保留报告原有发布日期字段，仅表示源日期及其原精度；不填充零点作为精确公布时间。无法取得的 `published_at`、`revision_id` 为 null。
- `data_version="current_visible_revision"` 表示查询时当前可见历史；不能据此进行“当时已知”的历史回测，也不提供完整 vintage 档案。US 原 provider 的版本标签另外保存在 `original` 中。

`original` 是适配前的完整 provider JSON 结果，保留原统计注释、方法变化、基期和来源字段。它**不是上游 HTTP 原始响应**；原始 HTML/CSV 的缓存策略仍由 provider 管理。新增适配不会改写原 provider 结果。

网络、解析、编码失败直接报错，不输出部分成功的数据集；状态字段不用于把请求失败伪装为数据缺失。当前不增加数据过期判定，沿用 provider 六小时缓存与失败处理；`available` 仅表示该观测有数值，不承诺它是最新公布期。

## 覆盖与后续迁移

当前已接入指标见上文各小节；尚未将所有宏观 provider 迁移。其余指标继续使用原 JSON，TODO 中全局基础项仍保留未完成状态。其他指标、部分抓取失败、完整历史版本与中美可比性需要各自的适配和测试，不因本批格式扩展而视为已完成。

测试覆盖真实离线样本、旧输出回归、官方值／计算值、M1口径边界、房地产范围变化、累计所属期、1—2月合并、零与缺失、严格基期、空结果、元数据空值、重复月份、非法范围、非有限值、上游错误、取消及输出写入失败。
