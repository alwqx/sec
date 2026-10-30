# 美国联邦财政收支

```bash
go run . macro us fiscal
go run . macro us fiscal --begin 198010 --end 202609
go run . macro us fiscal --begin 202510 --end 202610 --json --no-cache
```

## 数据来源与范围

采集美国财政部财政服务局 Monthly Treasury Statement（MTS）的三条月度系列，由圣路易斯联储 FRED 转接：

| 系列 | 指标 | JSON 月度字段 | 单位与符号 |
| --- | --- | --- | --- |
| MTSR133FMS | Total Federal Receipts | `receipts` | 百万美元 |
| MTSO133FMS | Total Federal Outlays | `outlays` | 百万美元 |
| MTSDS133FMS | Federal Surplus or Deficit [-] | `surplus_or_deficit` | 百万美元，盈余正、赤字负 |
| 计算展示值 | 官方余额的相反数 | `deficit_positive` | 百万美元，赤字正、盈余负 |

以上均为**所属自然月流量、现价、未季调、未折年**，不是财政年度累计，也不是国债余额、国债发行额或国民账户政府消费投资。终端仅四列：月份、收入、支出、赤字（负为盈余）；金额以百万美元显示，不把百万美元误当十亿美元。

使用官方独立盈余/赤字系列，不用收入减支出覆盖它。早期数据存在舍入差；不同系列修订进度也可能不同。退税、抵销收入、信贷项目重估等会影响收支，程序允许有限的负值及真实零，不强制将净支出改为正数，也不计算赤字的百分比增速或环比年化。

## 财政年度与累计

美国联邦财政年度为前一年 10 月至当年 9 月。例如 2025-10 属于 FY2026 第 1 月，2026-09 属于 FY2026 第 12 月，2026-10 属于 FY2027 第 1 月。每条观测保留 `fiscal_year` 和 `fiscal_month`。

JSON 的 `fiscal_year_to_date` 明确标记 `method=sum_of_current_snapshot_months`，按本份快照从财年 10 月至所属月逐月求和，可包含请求窗口以外的基期。各指标严格独立处理：其中任一月份缺值，该指标的累计即为 `null`，不跳过缺口、不以零代替缺失，10 月重新开始累计。累计盈余/赤字由官方月度余额逐月相加，赤字正值再取其相反数。

这是**当前月度快照的计算累计**，不是另行采集的官方财年累计，也不保证与某期 MTS 报告中印刷的累计数字一致：MTS 报告、FRED 各月的当前修订版本及独立系列可能不同。不要把它用作官方财年结算数、历史时点版本或融资需求的精确替代。

## 覆盖、缺失与修订

2026-10-29 核验合并 CSV：三条系列均从 1980-10 至 2026-09，共 552 月。运行时 `first_period` / `last_period` 从完整快照的非空观测计算，与查询范围分开。默认 UTC 上个月及此前 11 个月，默认开始月份最早截短至 198010；显式更早、无效、倒序或晚于当前 UTC 月的请求报错。

每个请求月份都有记录，尚未提供数值时为 `null`。空字符串或 `.` 是缺失，`0` 保留为零。`raw_values` 保存三个源字段的原始字符串。缺失本身不能证明官方尚未发布。

`data_version=current_visible_revision` 表示下载时可见的修订历史。原始 CSV 未提供逐期公布时间和修订编号，`published_at`、`revision_id` 为 `null`；`fetched_at` 仅为下载时间，不能用作历史回测的实际可得时间。MTS 通常在报告月后第八个工作日公布，但程序不根据这一通常规则猜测逐期公布时间。

## 接口、缓存与测试

`provider/fred/fiscal.go` 一次下载三个系列的完整 CSV：

```text
https://fred.stlouisfed.org/graph/fredgraph.csv?id=MTSR133FMS,MTSO133FMS,MTSDS133FMS
```

复用 FRED 客户端六小时原始快照缓存 `~/.sec/cache/fred/us-fiscal.json`。`--refresh` 跳过读取，`--no-cache` 不读写磁盘；命中和写入前均验证列名、月份、重复期间、每条系列有效观测、有限值及可计算累计的溢出。坏缓存重新下载；异常刷新保留原有效缓存；不静默回退过期快照。

`provider/fred/testdata/us-fiscal.csv` 为本次下载的完整源响应，统一 LF 换行。离线测试覆盖官方水平、单位与盈余/赤字符号、财年切换、严格累计缺口、零值和负值、独立系列舍入差、坏响应及异常刷新保护、缓存过期和未来时间戳、取消请求、命令 JSON 与紧凑表格。

```bash
go test ./provider/fred ./cmd/macro -run Fiscal -count=1
```

## 尚未完成：国债发行

本次仅完成财政收入、支出及盈余/赤字。国债发行仍为独立 TODO：官方 TreasuryDirect/FiscalData 的拍卖结果是可流通证券的拍卖口径，有拍卖日、发行结算日、发行量及投标获配量之分，并不自动等于含非流通证券的国债总发行、净融资或赤字。后续需明确覆盖证券、SOMA/政府账户分配、再发行、赎回与发行结算日后再接入，不能拿财政赤字或债务存量差冒充发行量。

已实时核验财政部拍卖 API `/v1/accounting/od/auctions_query` 可访问；响应中有尚未结算且 `total_accepted`、`soma_accepted` 缺失的记录，不能将缺失的中标额当作零或把拍卖日当作实际发行日。该源还分开保留 SOMA 等获配字段。本次不把尚未完成覆盖与口径校验的发行子项标为完成。

## 官方参考

- [FRED 联邦收入 MTSR133FMS](https://fred.stlouisfed.org/series/MTSR133FMS)
- [FRED 联邦支出 MTSO133FMS](https://fred.stlouisfed.org/series/MTSO133FMS)
- [FRED 联邦盈余或赤字 MTSDS133FMS](https://fred.stlouisfed.org/series/MTSDS133FMS)
- [财政部 Receipts & Outlays](https://home.treasury.gov/data/receipts-outlays)：MTS 与 DTS、债务存量报告的用途和通常公布时点。
- [Monthly Treasury Statement](https://fiscaldata.treasury.gov/datasets/monthly-treasury-statement/)：原始发布入口。
- [TreasuryDirect 拍卖查询说明](https://treasurydirect.gov/auctions/auction-query/auction-query-help/) 与 [财政部国债入口](https://home.treasury.gov/policy-issues/financing-the-government/treasury-marketable-and-non-marketable-securities/)：后续发行采集的官方候选来源。
