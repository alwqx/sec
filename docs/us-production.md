# 美国工业生产与产能利用率

本功能采集美国联邦储备委员会（Federal Reserve Board）的 G.17 月度季调数据，经 FRED 的公开 CSV 转接，不需要 API Key。工业生产衡量实际产出，产能利用率衡量产出相对可持续最大产能的使用程度，适合分别观察工业供给与产能压力。

## 命令

```bash
sec macro us industrial-production
sec macro us industrial-production --sector manufacturing --begin 202401 --end 202412
sec macro us capacity-utilization --sector total --begin 202401 --end 202412
sec macro us capacity-utilization --sector manufacturing --begin 202401 --end 202412 --json
sec macro us industrial-production --refresh
sec macro us capacity-utilization --no-cache
```

`--sector` 支持 `total`（默认）与 `manufacturing`。`--begin/--end` 使用 `YYYYMM`，默认截至 UTC 上个月，返回最近十二个月；默认开始月份按选中系列的起点截断。显式查询起点之前的月份会报错。未公布或缺失月份仍保留一行，水平与变化使用空值，终端显示 `—`。

## 官方系列与口径

| 指标 | 部门 | FRED ID | 历史起点 | 单位 |
| --- | --- | --- | --- | --- |
| 工业生产 | 工业总量 | [INDPRO](https://fred.stlouisfed.org/series/INDPRO) | 1919-01 | 指数，2017=100 |
| 工业生产 | 制造业（NAICS） | [IPMAN](https://fred.stlouisfed.org/series/IPMAN) | 1972-01 | 指数，2017=100 |
| 产能利用率 | 工业总量 | [TCU](https://fred.stlouisfed.org/series/TCU) | 1967-01 | 百分比 |
| 产能利用率 | 制造业（NAICS） | [MCUMFN](https://fred.stlouisfed.org/series/MCUMFN) | 1972-01 | 百分比 |

四个系列均为月度、季节调整后水平。制造业分项明确选择 NAICS 31–33 口径，不含伐木和出版业；美联储较宽的传统制造业口径还包含伐木及部分出版业（报纸、期刊、图书和目录出版，不含纯互联网出版），不能混用。工业总量覆盖这个较宽的制造业口径、采矿业、电力和燃气公用事业，不能将制造业与工业总量相加。指数与利用率都直接使用各自官方系列。口径依据[美联储G.17说明](https://www.federalreserve.gov/releases/g17/Current/default.htm)。

生产指数反映位于美国境内相关机构的实际产出，不区分所有权，不包括美国属地，单位不是金额。美联储目前采用 2017 为指数参考年，后续官方换基时需同步维护元数据。产能利用率没有指数参考年，其 `reference_year=null`，不能解读为 2017=100 指数。

美联储对产能的定义是：在现实工作安排、正常停工和投入充分的条件下，现有资本可维持的最大产出。利用率等于季调产出指数除以相关产能指数再乘以100。详见[官方说明](https://www.federalreserve.gov/releases/g17/About.htm)及[行业结构表](https://www.federalreserve.gov/releases/g17/SandDesc/src_desc1.htm)。

## 变化计算与缺失处理

生产指数使用同一季调序列计算：

- 同比：`(当月指数 / 上年同月指数 - 1) × 100`。
- 月环比：`(当月指数 / 上月指数 - 1) × 100`，不年化。

产能利用率仅计算百分点差：

- 较上年同月：`当月利用率 - 上年同月利用率`。
- 较上月：`当月利用率 - 上月利用率`。

例如利用率从75%上升到76%，变化为1个百分点。利用率不计算相对同比或环比增速。两类指标的水平都不折年。

严格按日历月份找基期，缺失二月时，三月不会改用一月作环比。`.` 或空字段保留为缺失，不填零、不插值。零值保留为有效水平；指数基期为零时增速为空，利用率基期为零时仍可计算百分点差。

## JSON、缓存和修订

`series_id/sector/coverage/unit/change_unit/reference_year/seasonally_adjusted/first_period` 说明所选系列的口径和单位。`observations` 保留 `period/level/raw_value`，`yoy_base/mom_base` 保留严格基期月份及其原值。生产指数填充 `yoy_percent/mom_percent`，利用率填充 `yoy_percentage_points/mom_percentage_points`，另一类变化字段为 `null`。缺失值也为 `null`。

统一下载[完整四系列CSV](https://fred.stlouisfed.org/graph/fredgraph.csv?id=INDPRO,IPMAN,TCU,MCUMFN)，缓存到 `~/.sec/cache/fred/production.json`，有效期六小时。不同部门、指标和查询范围共享这一快照，各列历史起点与缺失值分别处理。`--refresh` 强制重新获取；`--no-cache` 不读写缓存。下载或校验失败会返回错误，不覆盖已有缓存，也不将旧数据伪装为刷新结果。该缓存与 PCE、实际消费和失业金缓存分别保存。

美联储会随着更完整的月度资料和年度基准、季调因子等变化修订历史数据，见[官方修订说明](https://www.federalreserve.gov/releases/g17/OMB/2026/OMB_2026.htm)。本功能只表示 `data_version=latest-available`，不能据此还原历史时点上投资者已知的信息。CSV 不含逐期实际公布时间、修订编号或完整历史版本，`published_at/revision_id` 保持 `null`；`fetched_at` 仅为采集时间，月初日期是统计期标签。

离线测试样本 `provider/fred/testdata/production.csv` 于2026-10-28从上述官方地址下载，覆盖1919-01至2026-09，用于固定真实数值、历史起点和不同列缺失行为；运行命令仍查询官方当前快照，不把测试样本当作线上数据。
