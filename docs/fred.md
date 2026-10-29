# FRED：美国经济统计数据转接

FRED（Federal Reserve Economic Data）由圣路易斯联邦储备银行提供。`provider/fred` 当前采集BEA（Bureau of Economic Analysis，美国经济分析局）的PCE价格指数、实际个人消费支出及主要分项、实际可支配个人收入、实际GDP及实际进出口，BEA与美国人口普查局的月度名义进出口，美国劳工部ETA（Employment and Training Administration，就业与培训管理局）的初请、续请失业金周度数据，美联储的工业生产、产能利用率与M2货币余额，人口普查局与HUD的住宅建设和新房销售、BEA的设备投资、NAR的成屋销售，以及FHFA的房价指数。FRED是分发来源，JSON另行保留原始编制机构与来源链接。NAR为行业协会，成屋销售属于该协会统计，区别于其他政府机构编制的系列。

实际收入的单位、季调折年口径和命令见 [实际可支配个人收入](real-income.md)；工业总量与NAICS制造业系列、产能利用率及各自历史起点见 [工业生产与产能利用率](us-production.md)。有效联邦基金利率直接由 [纽约联储](nyfed.md) 采集。

季度实际GDP与官方环比年化增速见 [美国实际GDP](us-gdp.md)；月度M2季调余额与变化见 [美国货币供应](us-money.md)。联邦基金目标区间及政策事件见 [美联储政策利率](federalreserve.md)。

月度名义贸易与季度实际贸易的出口、进口、差额及商品/服务分项见 [美国进出口](us-trade.md)。两种频率与单位分别保留，季度链式实际分项不据此推算可加总的贸易总量。

住宅开工、建筑许可和设备投资见 [美国投资](us-investment.md)；新房销售、成屋销售和FHFA房价指数见 [美国房地产](us-housing.md)。月度销售与季度房价分别查询，保留各系列的实际覆盖范围及季调、折年和单位差异。

企业税前利润、商业库存和制造业新订单见 [美国企业部门](us-enterprise.md)，分别按季度季调折年流量、月末余额和月度流量采集，不混用频率与单位。

月平均银行信贷、SLOOS贷款标准净收紧比例和家庭偿债比率见 [美国信用](us-credit.md)，保留调查季度、当前DSR方法范围及百分比/百分点变化的区别。

## 使用方式

```sh
sec macro us pce
sec macro us core-pce
sec macro us pce --begin 202501 --end 202609
sec macro us core-pce --begin 202501 --end 202609 --json
sec macro us pce --refresh
sec macro us pce --no-cache
```

起止月份为 `YYYYMM`，包含端点。默认截至UTC上个月，共12个月；仅指定截至月时默认向前11个月，不早于195901。拒绝逆序、无效月份、早于195901和晚于当前UTC月的范围。未发布或缺失月份返回null，表格显示 `—`，不补值。

## 官方序列及价格口径

| 命令 | FRED系列 | BEA账户代码 | 定义 |
| --- | --- | --- | --- |
| `pce` | [PCEPI](https://fred.stlouisfed.org/series/PCEPI) | DPCERG | 个人消费支出链式价格指数 |
| `core-pce` | [PCEPILFE](https://fred.stlouisfed.org/series/PCEPILFE) | DPCCRG | 剔除食品和能源的个人消费支出链式价格指数 |

两条序列的官方定义均为**月度、季调、2017=100的链式价格指数**。原始发布是BEA的Personal Income and Outlays；[BEA价格指数说明](https://www.bea.gov/data/personal-consumption-expenditures-price-index)介绍了该指数衡量的消费价格变化。它们不是以美元计价的PCE消费金额，也不是实际消费量或链式数量指数；实际个人消费支出使用本页后述的独立系列及 `real-pce` 命令。

当前采集不依赖BEA或FRED API密钥，使用FRED公开CSV下载：

```text
GET https://fred.stlouisfed.org/graph/fredgraph.csv?id=PCEPI,PCEPILFE
```

不附带单位变换、频率聚合或增长率转换参数。一次获取两条系列的完整历史，再按请求月份筛选；因此每条记录及同比/月环比基期来自同一份CSV快照，两个命令、不同范围可共用缓存。2026-10-27实测历史自1959年1月至2026年8月，共812个月，CSV仅约20KB，无需按月或按年反复请求。FRED公开下载端点对默认sec/Go User-Agent会中断连接，采集器使用其接受的通用下载客户端标识；不依赖本机curl程序。

## 增速计算与缺失

- 同比（%）=（当月季调价格指数 / 上年同月季调价格指数 − 1）×100。
- 月环比（%）=（当月季调价格指数 / 上月季调价格指数 − 1）×100，未年化。
- 两项均为同一季调序列的计算值；与CPI命令采用NSA同比、SA环比的组合有区别，JSON保留季调标记。
- 计算基期严格为对应日历月份；当月值或基期缺失、基期为零时返回null，不跨缺口寻找替代值。
- 保留源字符串和数值；源空单元格、`.`表示缺失，真实0仍为0。表格指数显示三位小数、计算增速显示两位，JSON保留计算精度；不冒充BEA另行发布并四舍五入的增速。

CSV按列名选择PCEPI和PCEPILFE，支持列顺序变化及 `observation_date` / `DATE` 日期列。未知/重复列、非月初日期、重复月份、损坏CSV、空历史、负值/非有限数等异常会报错，避免将其他指标或频率误当作价格指数。HTTP或解析失败时整次查询失败，不静默返回部分数据。

## JSON与版本

顶层保留地区US、指标、FRED系列ID、BEA账户代码、月度频率、价格指数类型、季调状态、2017基期、请求范围、编制/分发机构、CSV/系列/BEA来源链接，以及该快照UTC抓取时间。

每条 `observations` 含 `period`、`index`、`raw_index`、`yoy_percent`、`mom_percent`、`yoy_base`、`mom_base` 和缺失说明。两个基期对象同样包含月份、指数及源文本，即使未返回基期数据也保留应使用的月份。

BEA可能修订历史值及调整价格指数参考年。结果标记 `data_version="latest-available"`，仅代表抓取时可见版本；CSV没有逐月实际公布时间、修订编号或历史版本档案，所以 `published_at`、`revision_id` 为null，不把抓取时间当作公布时间，也不承诺重建历史初值。当前2017参考年按上述FRED官方系列元数据核验，未从CSV数值推断。需要当时可见版本的回测仍需独立的版本档案采集。

## 缓存与验证

原始CSV、来源地址和抓取时间原子写入 `~/.sec/cache/fred/pce-prices.json`，有效期六小时。两个指标和不同月份范围复用同一快照。`--refresh`跳过读取并更新缓存；`--no-cache`不读写磁盘；过期、来源不符或无效CSV缓存会重新采集。失败不覆盖已有有效缓存，不回退过期值。

`provider/fred/testdata/pce-prices.csv` 为2026-10-27下载的官方完整快照。测试使用模拟HTTP，不依赖在线数据；覆盖两个价格系列及BEA代码、真实历史/增速、基期与未发布月份、缺口不跨越、空值/零值、列重排、非法响应、缓存/刷新/不缓存/恢复、取消请求，以及命令表格/JSON。实测命令与官方系列页面的最新月值一致。

## 初请与续请失业金

```sh
sec macro us initial-claims
sec macro us continued-claims
sec macro us initial-claims --begin 20260901 --end 20261024
sec macro us continued-claims --begin 20260901 --end 20261024 --json
sec macro us initial-claims --refresh
sec macro us continued-claims --no-cache
```

| 命令 | FRED系列 | 口径与单位 |
| --- | --- | --- |
| `initial-claims` | [ICSA](https://fred.stlouisfed.org/series/ICSA) | 常规州失业保险初请资格申请数，件，季调 |
| `continued-claims` | [CCSA](https://fred.stlouisfed.org/series/CCSA) | 续请所反映的参保失业人数， 人，季调 |

两条系列的官方频率为每周、周六结束，原始编制机构为美国劳工部ETA。初请是失业保险资格申请，不等同于全部新增失业人数；续请基于对应失业周的后续申领，覆盖申请常规州失业保险的人群，不等于CPS全部失业人口。[劳工部查询页面](https://oui.doleta.gov/unemploy/claims.asp)提供原始项目说明与周度查询。

两命令共用一份完整历史CSV：`https://fred.stlouisfed.org/graph/fredgraph.csv?id=ICSA,CCSA`，不附带单位变换、频率聚合或增长率转换参数。本次核验含1967-01-07至2026-10-17共3120个周六，CSV约81KB；其中ICSA最新为2026-10-17，CCSA最新为2026-10-10。合并CSV末行的CCSA为空，保留该空值，不将续请前移到初请最新所属周，也不把FRED更新日当成所属周。

此处日期参数是包含端点的 **`YYYYMMDD`**，按区间内的周六所属日期筛选。默认截至UTC今天，开始日期为截至日期之前最近12个周六中的第一个（截至日为周六时包含当天），最早19670107。可使用非周六边界，未包含周六的区间返回空列表。尚未发布的周六仍输出缺失行；初请与续请发布进度不同，默认范围尾部可能有不同数量的空行。只指定开始日期时截至UTC今天，只指定截至日期时仍默认最近12个周六。拒绝无效日期、逆序、早于历史起点及晚于UTC今天的范围。

JSON `observations` 每条包含：

| 字段 | 含义 |
| --- | --- |
| `week_ending` | 该周截至日期，`YYYY-MM-DD`，固定周六 |
| `count` / `raw_value` | 季调数量及FRED源单元格文本 |
| `weekly_change` | 当周 − 严格前7天的上周数量；缺少任一值即为null |
| `previous_week` | 上周的所属日期、数量和源文本 |
| `four_week_average` | 当周及前三个连续日历周的算术平均计算值 |
| `four_week_window` | 当周、前7/14/21天四个值对象，包含区间外基期与缺失状态 |
| `note` | 中文缺失或窗口不完整说明 |

四周均值明确标为计算值，不冒充另一个官方均值系列。当前周或任意一个严格窗口周缺失时，均值为null，不取“最近四条有效记录”替代。真实0有效，表格原始数量和周变化显示整数，均值显示两位小数，JSON保留计算精度。跨年按日历每7天取值，不用周编号拼接，不将周度数量简单累计为去重失业人数。CSV空单元格或 `.` 转为null；列重排、`DATE`或`observation_date`日期列均支持。未知/重复列、非周六或非法日期、重复周、空历史、非有限数、负数、非整数数量均在缓存前报错。

元数据包含地区US、系列ID、常规州项目范围、指标定义、单位、周频与周六标记、季调状态、编制/分发机构及来源链接。`latest_available_week` 是整份快照内该系列最后一个非空所属周，可能位于请求的历史区间之外；表格页脚明确标为“快照中本系列最新可用所属周”。原始数量、上周和四周窗口均来自同一CSV快照，使用顶层 `fetched_at`。

申领统计和季调历史可能修订；CSV不提供逐期的预估/修订状态、实际公布时间、修订编号或完整历史版本档案。结果为 `data_version="latest-available"`，`published_at`、`revision_id` 为null，不将抓取时间当成发布日或把历史查询当成当时已知版本。

CSV下载与校验后缓存逻辑与PCE共用，申领快照单独原子保存于 `~/.sec/cache/fred/jobless-claims.json`，有效期六小时。两项申领指标及所有日期范围共享该缓存，与PCE文件相互独立；`--refresh`重新下载，`--no-cache`不读写磁盘，失败不覆盖正常缓存，不回退过期值。

`provider/fred/testdata/jobless-claims.csv` 为本次核验保存的完整官方快照，仅用于离线HTTP模拟测试。测试覆盖真实最新周及发布滞后、连续四周均值、历史起点、跨年和区间外基期、空值/零值/整周缺失、日期及格式校验、缓存共享/刷新/失效恢复、不缓存、请求取消、PCE与申领缓存隔离，以及CLI表格、JSON和UTC默认日期。

## 实际个人消费支出

```sh
sec macro us real-pce
sec macro us real-pce --component goods
sec macro us real-pce --component services --begin 202501 --end 202609 --json
sec macro us real-pce --component durable-goods --refresh
sec macro us real-pce --component nondurable-goods --no-cache
```

默认 `--component total`。原始编制机构为BEA，发布于Personal Income and Outlays；[BEA消费支出主页](https://www.bea.gov/data/consumer-spending/main)提供调查范围与发布入口。采集下列五条月度官方序列，各自直接保留来源金额：

| 分项 | FRED系列 | BEA账户代码 |
| --- | --- | --- |
| `total` 总量 | [PCEC96](https://fred.stlouisfed.org/series/PCEC96) | DPCERX |
| `goods` 商品 | [DGDSRX1](https://fred.stlouisfed.org/series/DGDSRX1) | DGDSRX |
| `durable-goods` 耐用品 | [PCEDGC96](https://fred.stlouisfed.org/series/PCEDGC96) | DDURRX |
| `nondurable-goods` 非耐用品 | [PCENDC96](https://fred.stlouisfed.org/series/PCENDC96) | DNDGRX |
| `services` 服务 | [PCESC96](https://fred.stlouisfed.org/series/PCESC96) | DSERRX |

本次核验五条序列均为 **月度、十亿美元（2017年链式美元）、季调折年率（SAAR）**，历史自2007年1月起。链式实际金额衡量剔除价格变化后的消费水平，区别于名义消费金额，也区别于 `pce` / `core-pce` 的2017=100价格指数。这里2017为链式美元参考年，不是将 `level` 当成指数100。

按[BEA折年率说明](https://www.bea.gov/help/faq/121)，折年水平用于方便跨期比较。命令保留原始SAAR水平，不除以12，也不把该水平称为当月实际发生额。同比 =（当月 / 上年同月 − 1）×100，月环比 =（当月 / 上月 − 1）×100，均使用同一季调序列、同一CSV快照，**增速未年化**。不将月环比乘12或复利折成年率。表格水平显示一位小数、增速两位小数；JSON保留计算精度。

根据[BEA链式价格和数量方法](https://www.bea.gov/help/faq/521)及[发布说明中的不可加性注释](https://www.bea.gov/news/2024/personal-income-and-outlays-april-2024)，链式金额分项一般不具有可加性。程序直接取官方商品总量和官方消费总量，不以耐用品加非耐用品替代商品，不以商品加服务构造总量，也不以金额占比推算增长贡献度。缺少某分项时，不从其他系列求和或求差补齐。

日期参数为包含端点的 `YYYYMM`，默认截至UTC上个月、共最近12个月，最早200701。每个月均输出一项，未发布月份或源空单元格/`.` 保留null；真实0有效。同比或环比严格对应基期缺失、基期为0时，该增速为空；不跨缺口寻找替代值。

JSON顶层保存 `component`、系列ID、BEA账户代码、频率、实际消费定义、`unit`、`reference_year=2017`、`seasonally_adjusted=true`、`level_annualized=true`、`growth_annualized=false`、`components_additive=false`。可加性字段表示不能假设分项在所有时期可加。每条观测含 `period`、`level`、`raw_value`、`yoy_percent`、`mom_percent`、严格基期 `yoy_base` / `mom_base` 及中文缺失说明。它不输出价格指数 `index` 字段。

所有分项从同一个公开CSV端点获取完整历史：`https://fred.stlouisfed.org/graph/fredgraph.csv?id=PCEC96,DGDSRX1,PCEDGC96,PCENDC96,PCESC96`。本次快照为2007-01至2026-08共236个月。CSV表头按固定系列ID匹配，支持列重排和 `DATE` / `observation_date`；拒绝未知/重复/缺少列、非法/非月初日期、早于系列起点的日期、重复月份、空历史、负值或非有限数。错误响应不会更新正常缓存。

原始CSV在 `~/.sec/cache/fred/real-pce.json` 原子保存六小时；不同分项及日期范围共用，支持 `--refresh` 和 `--no-cache`，与PCE价格和申领数据缓存分开。价格指数、实际消费及申领数据均复用同一CSV下载/校验后缓存逻辑。

BEA可能修订历史及变更参考年。当前元数据按上述官方系列页面核验；结果为 `data_version="latest-available"`，仅代表抓取时可见版本，CSV不提供逐期实际公布时间、修订编号和完整历史版本档案，`published_at`、`revision_id` 为null。来源链接、编制/分发机构和UTC `fetched_at` 均保留。

`provider/fred/testdata/real-pce.csv` 为本次下载的完整官方快照，仅用于HTTP模拟测试。测试覆盖五个分项的原值、单位/参考年/折年标识、未年化增速、严格基期、起点及未发布月份、缺口/零值、不相加分项、不从其他分项补缺口、CSV校验、缓存共享和隔离、刷新和失败保护、请求取消，以及CLI表格/JSON和分项选择。
