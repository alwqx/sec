# 美国进出口与贸易差额

美国贸易数据提供两个独立命令，避免将月度名义金额与季度实际折年水平放入同一时间列。两者均通过 FRED 公开 CSV 采集，无需 API key。

```bash
# 月度名义贸易，默认商品与服务合计
sec macro us trade --begin 202501 --end 202608
sec macro us trade --component goods --begin 202501 --end 202608 --json
sec macro us trade --component services --refresh

# 季度实际贸易：2017年链式美元、季调折年
sec macro us real-trade --begin 2025Q1 --end 2026Q2
sec macro us real-trade --component goods --begin 2025Q1 --end 2026Q2 --json
sec macro us real-trade --component services --no-cache
```

`--component` 接受 `total`（默认合计）、`goods`（商品）、`services`（服务）。月度日期为 `YYYYMM`，表格显示 `YYYY-MM`；季度日期为 `YYYYQ1` 至 `YYYYQ4`。`trade` 默认查询 UTC 上个月及前 11 个月，最早 199201；`real-trade` 默认查询 UTC 上一完整季度及前 7 季，最早 1947Q1。默认截至期可能尚未公布，此时保留该期间，值为 `null`，不自动换成其他时间范围。

## 数据来源与覆盖

月度数据来自 Census 与 BEA 的 **U.S. International Trade in Goods and Services**，国际收支（BOP）口径、当期美元、季调、不折年，单位为**百万美元**。商品采用 BOP 调整后的统计，并非未经调整的 Census 口径，也不是 Advance Economic Indicators 的商品预估。

| 分类 | 出口 | 进口 | 官方差额 | 月度起点 |
| --- | --- | --- | --- | --- |
| 商品与服务合计 | [BOPTEXP](https://fred.stlouisfed.org/series/BOPTEXP) | [BOPTIMP](https://fred.stlouisfed.org/series/BOPTIMP) | [BOPGSTB](https://fred.stlouisfed.org/series/BOPGSTB) | 1992-01 |
| 商品 | [BOPGEXP](https://fred.stlouisfed.org/series/BOPGEXP) | [BOPGIMP](https://fred.stlouisfed.org/series/BOPGIMP) | [BOPGTB](https://fred.stlouisfed.org/series/BOPGTB) | 1992-01 |
| 服务 | [BOPSEXP](https://fred.stlouisfed.org/series/BOPSEXP) | [BOPSIMP](https://fred.stlouisfed.org/series/BOPSIMP) | [BOPSTB](https://fred.stlouisfed.org/series/BOPSTB) | 1992-01 |

注意：`BOPXGS`、`BOPMGS` 是已经停用的旧季度序列，不用于月度贸易。

季度实际数据来自 BEA 国民收入与产品账户（NIPA），发布于 GDP 统计，**2017 年链式美元、季调折年（SAAR）**，单位为**十亿美元**。折年水平不是该季度实际流量；增速没有折年。

| 分类 | 出口 | 进口 | 官方实际净出口 | 水平起点 |
| --- | --- | --- | --- | --- |
| 商品与服务合计 | [EXPGSC1](https://fred.stlouisfed.org/series/EXPGSC1) | [IMPGSC1](https://fred.stlouisfed.org/series/IMPGSC1) | [NETEXC](https://fred.stlouisfed.org/series/NETEXC) | 出口/进口 1947Q1；净出口 1970Q1 |
| 商品 | [A253RX1Q020SBEA](https://fred.stlouisfed.org/series/A253RX1Q020SBEA) | [A255RX1Q020SBEA](https://fred.stlouisfed.org/series/A255RX1Q020SBEA) | 本次未采集，保持 null | 1970Q1 |
| 服务 | [A646RX1Q020SBEA](https://fred.stlouisfed.org/series/A646RX1Q020SBEA) | [B656RX1Q020SBEA](https://fred.stlouisfed.org/series/B656RX1Q020SBEA) | 本次未采集，保持 null | 1970Q1 |

季度合计实际净出口直接读取官方 `NETEXC`。本次没有采集商品、服务各自的官方实际净额，因此不生成这些分项净额。BEA 链式美元分项通常不具有可加性，不通过相加分项还原合计，也不将分项相减默认为官方实际净额。覆盖起点以前保留 `null`；返回的 `series` 分别列明每个已采集序列的有效起止期。

月度与季度数据的单位相差 1000 倍，同时存在名义/实际、BOP/NIPA、非折年/折年的差异，不能直接拼接成单条序列，不能仅将月度加总后当成实际季度水平。

## 变化计算与缺失值

- 出口与进口同比：`(当期 / 上年同期 - 1) × 100`。月度基数是严格上一年同月，季度基数是严格上一年同季。
- 出口与进口上期增速：`(当期 / 上期 - 1) × 100`。月度对应月环比，季度对应季环比，均未年化。
- 差额变化：`当期官方差额 - 基期官方差额`，单位与水平一致。负数逆差是合法值，不对负基期计算百分比。变化为正表示余额增加，例如逆差由 -100 改为 -80，变化是 +20。
- 同比、上期比较所用的完整水平和原始字符串保留在 `yoy_base`、`previous_period_base`。基数可在请求窗口外，从同份快照取得。
- 严格基期缺失或为零时，出口/进口增速为 `null`；差额基期为零仍可计算金额变化。缺失不跨越，不插值，不回退至最近一条观测。
- 官方各序列可能存在独立舍入，差额直接读取官方序列。例如所收集快照中 2026-07 合计出口 310723、进口 403550，官方差额为 -92826；不以相减得到的 -92827 覆盖官方值。
- 缺失月份、季度与空值标记保留为 `null`，表格显示 `—`。JSON 的 `raw_exports`、`raw_imports`、`raw_balance` 保留源值字符串。

## 缓存、版本与溯源

原始 CSV 在 `~/.sec/cache/fred/us-trade.json` 和 `us-real-trade.json` 分别缓存六小时。先校验完整 CSV 的日期、列、有限数值与有效序列，再原子写入缓存；错列、重复日期、错误频率和非有限数值会报错。出口/进口拒绝负值，差额允许负值。缓存与来源 URL 绑定，未来时间戳不视为有效缓存。

`--refresh` 跳过读取缓存并更新；`--no-cache` 完全跳过磁盘缓存。请求失败或快照校验失败直接返回错误，不以过期缓存替代。两种命令共享每类的完整多序列快照，切换分类或查询窗口可以复用缓存。

JSON 明确记录 `frequency`、`price_basis`、`accounting_basis`、`unit`、`reference_year`、`seasonally_adjusted`、`level_annualized`、`growth_annualized`、来源、每个系列 ID 与覆盖范围、`fetched_at`。`fetched_at` 是采集时间；`published_at`、`revision_id` 保持 `null`，不能用采集时间冒充逐期公布时间。`data_version=current_visible_revision` 表示当前可见的修订后历史，没有承诺历史初值或完整 vintage 档案。

`provider/fred/testdata/us-trade-monthly.csv`、`us-trade-quarterly.csv` 是 2026-10-28 从上述 FRED 公开 CSV URL 获取的真实离线快照。网页缓存、CSV 与未来修订可能不同，测试按所存 CSV 原值校验。

## 官方说明

- [Census：季调名义历史数据与 1992 年起月度覆盖](https://www.census.gov/foreign-trade/statistics/historical/seas.html)
- [Census：统计方案与 BOP 调整说明](https://www.census.gov/foreign-trade/guide/sec2.html)
- [BEA：国际商品与服务贸易](https://www.bea.gov/data/intl-trade-investment/international-trade-goods-and-services)
- [BEA：NIPA Handbook 第八章，净出口](https://www.bea.gov/resources/methodologies/nipa-handbook/pdf/chapter-08.pdf)
- [BEA：链式美元定义及不可加性](https://www.bea.gov/help/glossary)

本次范围包括月度名义合计/商品/服务出口、进口、官方差额，以及季度实际合计/商品/服务出口、进口、官方合计净出口。商品/服务实际官方净额、独立月度实际商品贸易、历史发布版本与逐期公布时间仍属后续范围；不以现有序列推造这些数据。
