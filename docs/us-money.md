# 美国 M2 货币余额

`sec macro us money`（别名 `m2`）查询美国联邦储备委员会 H.6 的月度 M2 余额，经圣路易斯联储 FRED 分发。固定使用 [M2SL](https://fred.stlouisfed.org/series/M2SL)，不需要 API Key。

## 口径

| 项目 | 定义 |
| --- | --- |
| 指标 | M2 名义货币余额 |
| 频率 | 月度，月平均水平 |
| 季调 | 季节调整后 |
| 单位 | 十亿美元，非实际购买力、非指数 |
| 覆盖 | 1959-01 起 |
| 折年 | 水平和计算增速均不折年 |

M2 当前由 M1、小额定期存款（单笔小于10万美元）和零售货币市场基金余额相加，再扣除存款机构及货币市场基金的 IRA 和 Keogh 退休账户余额形成。月平均余额不是月末余额，不是当月新增，也不是美元流量。官方定义和历史范围见 [H.6 当前发布](https://www.federalreserve.gov/releases/h6/current/default.htm)及 [H.6 说明](https://www.federalreserve.gov/releases/h6/about.htm)。此命令直接使用官方 M2 总量，不自行加总其他系列，不与中国 M2 默认作数值排名。

## 查询

```bash
sec macro us money
sec macro us m2 --begin 202501 --end 202609
sec macro us money --begin 202609 --end 202609 --json
sec macro us money --refresh
sec macro us money --no-cache
```

日期为 `YYYYMM`。默认截至 UTC 上个月并返回最近12个月，默认开始月在系列起点处截断；显式开始月份早于195901或截至月份晚于当前 UTC 月会报错。尚未公布或源中缺失的月份保留一行 `null`，不使用最新值延伸填充。

同比为 `(本月 / 上年同月 - 1) × 100`，月环比为 `(本月 / 上月 - 1) × 100`，均基于同一份官方季调水平历史，计算值不年化。原始数值的舍入可能影响计算结果。基期严格按日历月份匹配，不跳过缺口；`.` 和空值保留缺失，真实零保留为0。基期缺失或为零时增速为空；本期为零而基期非零时可计算为-100%。

## 方法与修订

参照 [美联储 H.6 技术问答](https://www.federalreserve.gov/releases/h6/h6_technical_qa.htm)：

- 自2020年5月起，储蓄存款重分类进入 M1。这改变了 M1 的定义，但未改变已包含这些存款的 M2 总量；不为 M2 人为制造断点或跳过同比。
- 2026-07-28起，IRA 和 Keogh 账户余额改在 M2 总量层面扣除，替代以前在分项层面扣除的方法。调整追溯到各受影响系列起点；非季调 M2 总量不变，季调 M2 因扣除项的季调方法变化而有小幅修订。

当前结果使用官方重算后的完整历史，不把旧分项定义或先前下载值拼接到当前快照。`methodology_notes` 和 `methodology_source` 保留上述信息。官方后续仍可能修订历史；本功能不保存完整历史发布版本。

## JSON 与缓存

`level/raw_value` 保留数值和原始 CSV 单元格；`yoy_percent/mom_percent` 为计算值，`yoy_base/mom_base` 保留严格基期月份、水平和原文。元数据包含 `region=US`、`series_id=M2SL`、`measure=nominal-money-stock`、`period_basis=monthly-average`、单位、季调状态及来源。

`data_version=latest-available` 表示抓取时可见的修订历史。CSV 不含逐期实际公布时间、修订编号或历史版本，因此 `published_at/revision_id=null`；`fetched_at` 只记录下载时间。月初日期只是所属月份标签，不作为公布日期。

一次下载 [官方完整 CSV](https://fred.stlouisfed.org/graph/fredgraph.csv?id=M2SL)，校验后原子缓存到 `~/.sec/cache/fred/money.json`，有效期6小时。不同日期范围共享快照，与其他 FRED 指标缓存隔离。`--refresh` 重新下载并更新成功缓存；`--no-cache` 不读写磁盘。缓存过期、来源不符或内容损坏时重新采集；网络或校验失败返回错误，不回退过期结果、不覆盖正常缓存。

离线样本 `provider/fred/testdata/money.csv` 于2026-10-28下载，覆盖1959-01至2026-09共813个月。2026-09为23413.9，2026-08为23300.9，2025-09为22149.2。样本用于测试，运行命令继续获取官方当前快照。
