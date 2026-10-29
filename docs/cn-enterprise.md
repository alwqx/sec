# 中国工业企业经营指标

`sec macro cn enterprise` 采集国家统计局发布库中的全国工业企业总计，提供累计利润、月末存货、月末产成品存货、月末应收账款及各自的官方同比。金额单位为亿元，保留当期账面和现价数值，未作价格或季节调整。

```bash
sec macro cn enterprise
sec macro cn enterprise --begin 202601 --end 202609 --json
sec macro cn enterprise --metric profit --begin 200612 --end 200705
sec macro cn enterprise --metric receivables --begin 201812 --end 202002 --no-cache
sec macro cn enterprise --metric finished-goods --begin 202601 --end 202608 --refresh
```

`--metric` 可选 `all|profit|inventory|finished-goods|receivables`，默认 all。`--begin`、`--end` 使用 `YYYYMM`，默认截至上个月的最近 12 个月；仅传较早的 `--end` 时，默认起点会收缩至已支持的 `199901`，显式指定更早 `--begin` 仍拒绝。表格显示 `YYYY-MM`，仅保留所属月、指标、统计期、金额和官方同比五列；调查范围及逐条说明保留在 `--json` 输出中，避免长文本撑宽终端表格。支持 `--json`、`--refresh`、`--no-cache`；`--refresh` 跳过已有缓存并更新，`--no-cache` 不读写缓存。

## 官方接口与已核验覆盖

动态发现路径是 `月度数据 → 工业 → 工业企业主要经济指标`，通过现有 `queryIndexTreeAsync` 查询目录，不选择大中型、国有控股或行业分组目录。2026-10-28 核验时目录 ID 为 `d760d189206642ce8afad57640408e09`，元数据接口 `new/queryIndicatorsByCid?cid=…&pageSize=1000` 返回 76 个指标。

按完整指标名、单位、目录 ID 以及表示累计/本月末/同比的 `dp` 属性选择系列。默认一次 `stream/esData` 请求获取全国代码 `000000000000` 的八个系列，不硬编码系列 ID 作为查询入口；下表 ID 仅记录本次核验结果。

| metric | 金额系列 ID | 官方同比系列 ID | 已核验最早非空所属月 |
| --- | --- | --- | --- |
| profit | `5d98d24036dd4dbebf7f5c0a80753df6` | `7f30595d047445e0a3880ab928795892` | 1999-02 |
| inventory | `9e0537fd94264be0a0f0c1d239dac6fd` | `9b524e0c28224feaad7f1f008890a0dc` | 2010-02 |
| finished-goods | `f67df29d7067437f88bcc2dbce9ef677` | `e00c23ba6ffc45ae86757ce81f1c1997` | 2001-02 |
| receivables | `46196614c8a64cfdbde9c2e5661083fe` | `01d4fd4cd8ec4be5bf836d89cb2ebe0e` | 2001-02；2019 年中断 |

本轮核验查询了 1998-01 至 2026-09。支持的查询起点为 `199901`；较早请求拒绝，避免把未核验口径包装成已支持历史。各系列早于其已核验非空起点的单元格为空时照实保留，并说明缺口。`verified_first_nonempty` 是本轮查询观察到的首个非空所属月，并非官方承诺的覆盖起点；官方元数据本身的 `daterange_sdate`、`daterange_edate` 为空，JSON 原样保留 `null`。不保证这些年份之间每月都有数值。

截至 2026-10-28，八个系列最新非空为 **2026-08**，2026-09 单元格均为空，但官方 2026 年 1—9 月利润月报已于 2026-10-27 发布。说明发布库可能滞后：命令不拿新闻稿覆盖发布库结果，也不把空值解释成“官方尚未发布”。可稍后通过 `--refresh` 重新查询。

## 流量、存量与同比

| metric | 金额字段的含义 | `measurement` | 时间字段 | 官方同比含义 |
| --- | --- | --- | --- | --- |
| profit | 年初至所属月累计利润总额 | `year_to_date_flow` | `ytd_period=YYYY01-YYYYMM`，`as_of_date=null` | 累计利润的可比口径同比 |
| inventory | 所属月末全部存货账面余额 | `month_end_stock` | `as_of_date` 为月末日期，`ytd_period=null` | 本月末相对上年同期的官方同比 |
| finished-goods | 所属月末产成品存货余额 | `month_end_stock` | 同上 | 月末余额的官方同比 |
| receivables | 所属月末应收账款余额 | `month_end_stock` | 同上 | 月末余额的官方同比 |

库存包括原材料、在产品、半成品、产成品、商品等；产成品存货是其中一部分，不能替代全部存货，也不能与全部存货相加。定义和原始名称保留在 `metrics[].value_series` 等元数据中。

1 月免报。2 月利润是 1—2 月合并累计，**不是 2 月单月利润**；2 月库存和应收账款是 **2 月末余额**，不是两个月余额相加或平均。命令不推算单月利润，不相减累计同比，不插值，不用缺失值补零，也不用另一种经济含义的系列填缺口。

利润总额可以为负，利润同比可低于 -100%，不套用余额的非负规则。存货及应收账款金额不允许为负。若官方利润同比标为“注”“（注）”等不可计算标记，保留同比 `null` 及说明，不将其变为零；不根据亏损基数自行计算同比。

## 调查范围和会计列报断点

每条观测同时保留 `scope_standard`、中文 `survey_scope`、`comparison_scope_year` 与 `survey_frequency`，避免静默连接范围不同的历史：

| 所属期 | 规模以上工业企业范围 | 代码 |
| --- | --- | --- |
| 1998—2006 年；本命令提供 1999 年起已核验部分 | 全部国有，以及年主营业务收入 500 万元及以上的非国有工业法人单位 | `all_state_owned_and_nonstate_revenue_5m` |
| 2007—2010 年 | 年主营业务收入 500 万元及以上工业法人单位 | `industrial_legal_entities_revenue_5m` |
| 2011 年起 | 年主营业务收入 2000 万元及以上工业法人单位 | `industrial_legal_entities_revenue_20m` |

2007—2010 年财务状况调查仅在 **2、5、8、11 月**进行，其他所属月的缺口标为 `not_surveyed`，不插值。2011 年起按月全面调查，1 月免报。2007 年所有制覆盖调整、2011 年收入门槛提高以及调查频率变化均构成比较断点；不能把这些范围当成一组固定企业。

即使门槛相同，每年的调查名录也会变动：企业进退规、新建或注销、执法清理、基数修正、跨地区跨行业重复统计剔除均影响可比范围。官方同比使用当年可比范围的上年同期数，不是把本期值除以上一年当时公开的金额。`comparison_scope_year` 只标识所属年度，并不宣称掌握逐家企业名录或修订基数；不自行“还原”官方同比。

应收账款另有会计列报断点：

- 2018 年及以前本系列为单列应收账款。
- 2019 年官方停止发布月度应收账款，改为应收票据及应收账款。本命令在该年保留应收账款缺口，非 1 月空值标为 `definition_suspended`，**不使用合并系列补值**。
- 2020 年起根据一般企业财务报表格式调整，重新分别列示应收票据、应收账款，官方恢复发布月度应收账款。`accounting_scope=separate_accounts_receivable_since_2020` 保留该段；其同比仍直接取官方值，不拿 2019 年合并金额作为分母。

## JSON、缺失和缓存

结果包含 `metrics` 元数据和按月份、指标排列的 `observations`。每个指标值用 `value` 表示亿元，`yoy_percent` 为官方百分数；`ytd_period` 与 `as_of_date` 按流量和存量互斥。全局记录单位、来源、抓取时间、未季调标记和范围说明。

| status | 含义 |
| --- | --- |
| available | 金额、官方同比均有值 |
| partial | 金额或同比缺失，包括同比不可计算标记 |
| no_value | 库中无值，可能为历史未收录、尚未更新等；不能断言官方尚未发布 |
| january_exempt | 1 月免报，不拆出 1—2 月合并值 |
| not_surveyed | 2007—2010 年非 2、5、8、11 月调查缺口 |
| definition_suspended | 2019 年应收账款被合并列报系列替代，保留原系列缺口 |

`published_at` 明确为 `null`：`esData` 不提供逐条公布时间。`historical_version` 也为 `null`，不能把当前库值当成某个历史时点当时可得的数据。`fetched_at` 是抓取时间，不能替代公布时间。

复用六小时 NBS 缓存，并通过 `requestValidated` 在缓存命中及网络写入前校验元数据与观测。缺少成功标记、列表不完整、指标重复、单位/时点属性变化、重复月份/全国单元格、非法数值、负余额或月份行缺失均报错，不覆盖原有效响应。官方空单元格可表示缺失；整行或所选全国指标单元格消失则视为接口响应不完整。坏缓存重新请求，过期缓存不在上游错误时静默降级。

离线样本来自真实官方目录、76 个指标元数据及历史 `esData` 响应。观测样本按原始返回字段截取完整月份窗口，不修改数值，覆盖首个已核验年份、2007/2011 范围和频率切换、2019 会计列报缺口以及当前库延迟。文本统一 LF 和 EOF 换行。

```bash
go test ./provider/nbs ./cmd/macro -run Enterprise -count=1
```

## 官方口径依据

- [国家统计局：工业统计常见问题](https://www.stats.gov.cn/hd/cjwtjd/202302/t20230207_1902277.html)：历史规模门槛、2007—2010 年调查频率、按当年名录计算可比同比。
- [国家统计局：规模以上工业企业范围历史](https://www.stats.gov.cn/zt_18555/zthd/lhfw/2021/rdwt/202302/t20230214_1903909.html)：1998—2006、2007—2010、2011 年起三个统计范围。
- [2019 年 1—2 月工业企业利润](https://www.stats.gov.cn/sj/zxfb/202302/t20230203_1900265.html)：2019 年停止月度应收账款单列，改发应收票据及应收账款。
- [2020 年 1—2 月工业企业利润](https://www.stats.gov.cn/xxgk/sjfb/zxfb2020/202003/t20200327_1767778.html)：2020 年恢复应收账款单列、可比范围说明与 1 月免报制度。
- [2026 年 1—9 月工业企业利润](https://www.stats.gov.cn/sj/zxfbhjd/202610/t20261027_1965697.html)：当前利润、存量指标解释、官方同比与统计调查范围。
