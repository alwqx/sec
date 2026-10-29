# 美国企业利润、商业库存和制造业新订单

三个命令分别提供不同频率、统计总体和经济含义的数据，不拼接成一条“企业景气”序列，不与中国指标作直接排名。数据由 BEA、Census 编制，经 FRED 公开 CSV 下载，无需 API key。

```bash
sec macro us corporate-profits --begin 2025Q1 --end 2026Q3
sec macro us business-inventories --begin 202501 --end 202609
sec macro us new-orders --begin 202501 --end 202609 --json
sec macro us corporate-profits --refresh
sec macro us business-inventories --no-cache
```

## 系列和经济含义

| 命令 | FRED 系列 | 频率与金额单位 | 季调/折年 | 已核验起点 |
| --- | --- | --- | --- | --- |
| `corporate-profits` | `CPROFIT`，BEA 账户 `A051RC` | 季度，十亿美元 | 季调折年 SAAR | 1947Q1 |
| `business-inventories` | `BUSINV` | 月末余额，百万美元 | 季调、未折年 | 1992-01 |
| `new-orders` | `AMTMNO` | 月度流量，百万美元 | 季调、未折年 | 1992-02 |

**利润**取 BEA NIPA 的企业税前利润，包含存货估价调整（Inventory Valuation Adjustment，IVA）和资本消耗调整（Capital Consumption Adjustment，CCAdj），对应当期生产所得。IVA 将存货耗用按当期成本计价，CCAdj 将折旧按当期成本计价。这些调整不意味着数值已剔除通胀：金额仍为当期美元，`price_basis=current_dollars`。

该系列覆盖上市与非上市企业及 NIPA 其他公司组织，包括世界其他地区的净利润，不能等同于上市公司财报利润、税后利润、仅境内企业利润或经营现金流。水平是季度季调折年流量，不是当季实际利润，也不是年初累计值。JSON 另列 `tax_basis=before_tax`、`bea_account_code=A051RC`。

**商业库存**是制造业、商人批发业与零售业月末账面余额，不是全经济所有企业库存、库存销售比或 GDP 中的库存投资流量。JSON 保留 `measurement=month_end_stock` 与所属月最后一天的 `as_of_date`。FRED 元数据说明自 2001-06-14 起数据重编以反映 SIC 转 NAICS；下载的修订历史不表示当时已经使用相同分类。

**制造业新订单**取 Census M3 的制造业总量，表示当月新接订单扣除取消额，区别于未完成订单余额、发货量和 PMI 新订单扩散指数。半导体行业新订单不可得；主要即时交付行业不直接调查订单，相关估计方法应结合 M3 文档解读。此金额未折年，不擅自乘以 12 或与月末库存相加。

## 日期、变化和缺失

- 利润用 `YYYYQ1` 至 `YYYYQ4`，默认 UTC 上一完整季度及此前 7 季。
- 库存和订单用 `YYYYMM`，默认 UTC 上个月及此前 11 个月，终端显示 `YYYY-MM`。
- 默认窗口在各自起点截短；显式早于起点、倒序范围或晚于当前 UTC 月/季的请求报错。默认截止期不保证已经发布。
- 当前窗口每个月/季都保留；不存在的日期行或空单元格返回 `level=null`，终端显示 `—`，不插值或回退到最近有值的期间。
- 同比为 `(当期/严格上年同期 - 1) × 100`；环比为 `(当期/严格上期 - 1) × 100`。增速均未年化，利润水平采用 SAAR 不改变这一点。
- 基期从同一完整快照读取，可以在请求窗口外。基期缺失、为零或负数时增速为空，JSON 保留严格基期和缺失说明。
- 利润允许负值；正基数转亏损时可出现低于 -100% 的增速。库存和订单金额允许零、拒绝负值；所有源值及可计算增速必须有限。
- `raw_level` 保留原值字符串，空字符串和 `.` 缺失标记原样保留，真实零值不转为空值。

终端只有所属期、水平、同比和环比四列；长定义、范围、方法和逐条说明放在 `--json` 中。表格上方标明单位与季调/折年方式，下方提示来源、动态覆盖及查看详情的方法。

## 覆盖、修订和缓存

2026-10-29 下载的完整公开快照：利润 318 季，1947Q1 至 2026Q2；库存 416 月，1992-01 至 2026-08；新订单 415 月，1992-02 至 2026-08。运行时 `first_period`、`last_period` 按非空数值计算，独立于请求窗口。缺失不等于官方尚未发布，也可能反映分发滞后或源缺口。

数据标记为 `data_version=current_visible_revision`。没有逐期实际发布时间或修订批次时，`published_at`、`revision_id` 为 `null`；`fetched_at` 仅为下载时间，不可替代发布时间用于历史时点回测。

三个系列独立下载并缓存六小时：

| 指标 | 下载 URL 的 `id` | `~/.sec/cache/fred/` 中的文件 |
| --- | --- | --- |
| 利润 | `CPROFIT` | `us-enterprise-profit.json` |
| 库存 | `BUSINV` | `us-enterprise-inventory.json` |
| 订单 | `AMTMNO` | `us-enterprise-new-orders.json` |

下载 URL 为 `https://fred.stlouisfed.org/graph/fredgraph.csv?id=<id>`。单系列请求保留原始统计频率；FRED 将 BUSINV 的“Monthly, End of Period”和 AMTMNO 的“Monthly”组合下载返回 ZIP，故不依赖组合 CSV。

复用 `Client.csvSnapshot`，在命中缓存及写入前校验完整 CSV 的列、日期、频率、重复期间、有效观测、数值与可计算的严格基期增速。坏缓存重新采集；异常刷新不覆盖有效缓存，下载失败报错，不静默使用过期数据。`--refresh` 跳过已有缓存并更新；`--no-cache` 不读写磁盘。离线样本保留三个独立公开 CSV 的完整原值，统一 LF 换行。

```bash
go test ./provider/fred -run Enterprise -count=1
go test ./cmd/macro -run USEnterprise -count=1
```

## 官方来源

- [FRED CPROFIT](https://fred.stlouisfed.org/series/CPROFIT)：系列、单位、季调折年与 BEA 账户代码。
- [BEA NIPA Handbook 第十三章](https://www.bea.gov/resources/methodologies/nipa-handbook/pdf/chapter-13.pdf)：税前利润、当期生产所得、IVA/CCAdj 与境内/全国范围。
- [FRED BUSINV](https://fred.stlouisfed.org/series/BUSINV)：月末库存、单位和 NAICS 重编说明。
- [Census MTIS 调查说明](https://www.census.gov/mtis/about_the_surveys.html)：制造业、批发业与零售业统计总体。
- [FRED AMTMNO](https://fred.stlouisfed.org/series/AMTMNO)：制造业新订单总量与半导体缺口。
- [Census M3 定义](https://www.census.gov/manufacturing/m3/definitions/index.html)：新接订单扣除取消额、库存和未完成订单的区别。
