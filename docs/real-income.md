# 美国实际可支配个人收入

`sec macro us real-income` 查询 BEA 的实际可支配个人收入，经圣路易斯联储 FRED 分发。先观察 US 本国居民收入变化，不进行 CN/US 汇率换算或跨资产比较。

## 定义和来源

可支配个人收入等于个人收入减去个人当期税，是可用于消费或储蓄的收入；“实际”表示已扣除价格变动影响。这里采集全体个人的收入总量，不是人均收入、工资收入、名义金额，也不是实际个人消费支出。

| 项目 | 口径 |
| --- | --- |
| FRED 系列 | DSPIC96（Real Disposable Personal Income） |
| BEA 账户代码 | A067RX |
| 频率和覆盖 | 月度，自 1959-01 起 |
| 水平单位 | 十亿美元，2017 年链式美元 |
| 季调和折年 | 季调折年率（SAAR） |
| 参考年 | 2017；不是 2017=100 的价格指数 |
| 数据版本 | 当前下载时可见的最新修订历史 |

官方资料：[FRED 系列及元数据](https://fred.stlouisfed.org/series/DSPIC96)、[FRED CSV](https://fred.stlouisfed.org/graph/fredgraph.csv?id=DSPIC96)、[BEA 个人收入](https://www.bea.gov/data/income-saving/personal-income)、[BEA 定义与统计约定](https://www.bea.gov/news/pio-release-additional-information)。该系列属于 BEA 的 Personal Income and Outlays，相关表为 NIPA 2.6。

SAAR 是水平的计量口径，不能把它解释为当月实际收到的收入。链式美元是实际金额而非价格指数；参考年和历史数据都可能被官方修订。

## 查询

```bash
# 默认查询 UTC 上个月及其之前 11 个月
sec macro us real-income

# 显式范围、JSON 及缓存控制
sec macro us real-income --begin 202501 --end 202608
sec macro us real-income --begin 202608 --end 202608 --json
sec macro us real-income --refresh
sec macro us real-income --no-cache
```

月份必须为 `YYYYMM`，开始月份不早于 `195901`，截至月份不晚于当前 UTC 月。默认开始月份在源起点处截断。近期月份尚未公布时仍保留该行，水平和增速为缺失；不会将旧值延伸成新数据。

同比为 `(本月水平 / 去年同月水平 - 1) × 100`；月环比为 `(本月水平 / 上月水平 - 1) × 100`。两者使用同一官方季调水平序列，**增速未年化**。不会乘 12 或做 12 次方折年，不会拿名义收入、其他频率或人均系列替代。由于官方金额经过舍入，计算值可能与 BEA 用未舍入数据或数量指数公布的增速略有差别。

只取严格日历基期，不跳过缺口。CSV 中的 `.`、空值、源中缺少的月份都保留为 `null`，真实零值保留为 `0`。本期缺失或基期缺失/为零时，增速为 `null`；零本期及正数基期可以产生 -100%。

## JSON 和缓存

`level` 是官方水平，`raw_value` 保留原始文本。`yoy_percent`、`mom_percent` 是计算增速，`yoy_base`、`mom_base` 保留严格基期月份、水平和原文。元数据包含系列、BEA 代码、单位、参考年、季调及水平折年标志；`growth_annualized=false`。

`source`、`series_source`、`original_source` 追踪下载、FRED 元数据和 BEA。`fetched_at` 只是本次快照获取时间；CSV 不提供逐期实际公布时间、修订编号、完整历史版本，因此 `published_at`、`revision_id` 为 `null`，`data_version=latest-available`。本功能不能用于重建历史某日当时可见的收入数据，不把最新修订值当成当时已知值。

整份经过指标校验的 CSV 快照缓存于 `~/.sec/cache/fred/real-income.json`，有效期 6 小时，与实际 PCE、PCE 价格和失业金申请缓存隔离。不同查询范围共享该快照。`--refresh` 强制重新下载，`--no-cache` 不读写缓存；过期、源不匹配、损坏或结构不合法的缓存会重新请求。请求失败直接报错，不回退过期数据，也不覆盖已有有效缓存。

## 离线样本

`provider/fred/testdata/real-income.csv` 于 2026-10-28 从上述官方 CSV 下载，包含 1959-01 至 2026-08 共 812 个月；2026-08 为 18410.5，2026-07 为 18415.4，2025-08 为 18174.0。样本只用于确定性测试，运行查询仍通过 FRED 获取最新快照，不将样本硬编码为生产数据。
