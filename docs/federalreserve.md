# 美联储联邦基金目标利率区间

`federalreserve` 指美国联邦储备委员会（Federal Reserve Board）。
`provider/federalreserve` 采集委员会官网的政策目标区间及调整事件。
`sec macro us target-rate` 用于观察政策利率变化；可用别名 `fed-funds-target`。

## 官方来源与采集方法

1. [Open Market Operations](https://www.federalreserve.gov/monetarypolicy/openmarket.htm)
   的 **FOMC's target federal funds rate or range, change (basis points) and level**
   表提供各次调整的生效日期、加息/降息基点，以及新目标区间。采集器解析这张完整表，
   按生效日期升序排列，校验日期、区间上下限和相邻调整基点的一致性。
2. [FOMC会议日历](https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm)
   提供正式 Statement 的 HTML 链接。日历不再包含所需年份时，读取官方历史页
   `https://www.federalreserve.gov/monetarypolicy/fomchistoricalYYYY.htm`。
   只使用正式 Statement 标签或 Statement 栏内的 HTML 链接，排除同日长期目标、缩表计划等辅助公告。
3. 仅对查询区间内的调整及一个区间前基准，查找生效日前七天内的官方声明候选。
   从声明正文核验公布日期、新目标区间和具体公布时间。
   URL中的日期只缩小候选范围；最终公布日期必须与声明正文一致。
   必须唯一匹配目标区间，才写入公布信息。默认查询不会逐一下载全部历史会议材料。

无需 API key。数据源均为美联储委员会官网。
FRED 的 `DFEDTARL`、`DFEDTARU` 是日度有效目标区间序列；其日标签不等于政策公布日期，
本实现直接使用官方事件表及声明，不用日度标签推导公布信息。
有效联邦基金利率 EFFR 是另一种市场成交指标，另见 `sec macro us effr`。

## 日期与数值口径

- 支持 **2008-12-16** 起的目标区间机制，`--begin` / `--end` 使用 `YYYYMMDD`，
  按官方生效日期闭区间筛选。默认截至UTC今天，开始日期为截至日期前29天，共30个自然日。
  默认开始日期会在机制起点处截断；显式请求更早日期会报错。
- `events` 只包含实际调整。不会为每日、周末、假期或维持利率的会议补行。
- `baseline` 是严格早于开始日期的最近一次调整，可早于查询区间。
  `current` 是截至查询结束日期已经生效的目标区间；即使所选期间无调整，也返回有效区间，
  并保留其最初生效日期。尚未生效的调整不会提前应用。
- `effective_date` 来自官方调整表。`announced_date`、`published_at` 来自通过核验的声明。
  声明时间保留官方 EST（UTC−05:00）或 EDT（UTC−04:00）时区，不转换为本机时区。
  **不会把生效日期统一减一天，也不会把会议日期当成公布日期。**
- 2020-03-03 的紧急声明在10:00 EST公布，对应目标区间于 **2020-03-04** 生效；
  官方调整表在2024-07-10修正过该日期。2020-03-15周日17:00 EDT公布的紧急声明，
  对应 **2020-03-16** 生效。这些情况均由官方原文匹配，不能依赖常规会议日程推断。
- 目标利率单位为 `%`，变化单位为基点：1个百分点=100基点。
  上下限变化分别比较上一实际调整，前值可以在查询区间外。
  首条2008-12-16记录由单一目标切换为区间，保留官方降息 `75-100` 基点原文，
  两个计算变化字段为 `null`，避免跨机制比较。
- 历史为**当前可见版本**（`data_version=latest-available`），并非当时可得的历史快照。
  不用抓取时间替代公布时间，也不提供当时可得版本的回测保证。

## 公布信息缺失与失败处理

`release_status` 为：

| 状态 | 含义 |
| --- | --- |
| `available` | 已唯一匹配声明，获得公布日期和具体时间 |
| `date-only` | 已唯一匹配声明并获得日期，未取得可解析的具体时间 |
| `unavailable` | 未找到、下载失败、格式不支持、日期或区间不符，或匹配存在歧义 |

缺失的 `announced_date`、`published_at`、`statement_fetched_at`、`statement_raw` 以 `null` 表示，
`release_note` 解释原因。公布信息不可用时，官方调整表仍可提供完整的区间及生效日期；
终端明确显示“未取得”及原因，JSON保留缺失状态，不把猜测写入字段。
主调整表下载失败或未通过校验时，整个查询失败。取消上下文时立即返回取消错误。

旧声明或未来网页结构可能无法自动解析；这时保留真实缺失，后续可按官方结构扩展解析器。
例如只写“立即公布”的声明不提供可核验的具体时刻，`published_at` 保持 `null`。

## JSON与缓存

结果带有 `region=US`、`indicator=target-rate`、`frequency=event`、单位及官方源地址。
各事件保留原始年/月日、加息、降息和区间字符串；匹配成功时还保留声明原日期、
时间行、决定段落、声明链接及该页抓取时间。主表抓取时间与声明抓取时间分开记录。

原始HTML按来源URL缓存到 `~/.sec/cache/federalreserve/`，有效期6小时。
缓存命中仍重新校验内容、来源和时间；未来时间戳、损坏内容或过期缓存会重新请求。
所有页面必须通过解析校验后才使用临时文件原子替换缓存。
下载或校验失败不会覆盖原缓存，也不会静默回退为过期数据。
`--refresh` 跳过缓存读取并更新有效响应，`--no-cache` 完全跳过缓存读写。

```bash
sec macro us target-rate
sec macro us target-rate --begin 20200301 --end 20200331
sec macro us target-rate --begin 20081216 --end 20081231 --json
sec macro us target-rate --begin 20260901 --end 20261028 --refresh --json
sec macro us fed-funds-target --no-cache
```

## 验证

离线测试使用官方调整表完整样本及精简声明样本，覆盖区间机制起点、
2020紧急降息、EST/EDT、独立公布/生效日期、区间前基准、无调整区间、
未来生效事件、辅助公告排除、失效及损坏缓存、失败不覆盖缓存和上下文取消。
真实联网检查另行运行 CLI；常规测试不依赖联网。
