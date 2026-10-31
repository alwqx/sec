# 美国国债现金发行、现金赎回与调整

`sec macro us debt-cash` 采集财政部[Daily Treasury Statement（DTS）](https://fiscaldata.treasury.gov/datasets/daily-treasury-statement/)中的两部分数据：表II列示并转引IIIB的**官方现金发行和现金赎回总额**，以及IIIB的**记账交易转现金口径调整明细**。

```bash
sec macro us debt-cash
sec macro us debt-cash --begin 20260901 --end 20260930
sec macro us debt-cash --begin 20051003 --end 20051003 --json
sec macro us debt-cash --refresh
sec macro us debt-cash --no-cache
```

日期参数为 `YYYYMMDD`，按DTS记录日查询，默认UTC今天及此前6天，最早2005-10-03。周末、假期和尚未发布的日期不生成零值。两种数据源均无记录的日期不列示；仅一侧或一份来源有记录时保留可用数据并注明缺失。

## 官方现金总额

从 `deposits_withdrawals_operating_cash` 接口精确查询：

- `Deposits` 的 `Public Debt Cash Issues (Table IIIB)`：现金发行。
- `Withdrawals` 的 `Public Debt Cash Redemp. (Table IIIB)`：现金赎回。

兼容早期 `Table III-B` 类别和 `Federal Reserve Account` 账户名；当前账户为 `Treasury General Account (TGA)`。同日同侧出现新旧类别或账户的多份总额会报错，不混加。原账户名、类别及源行号保存在JSON原始字段中。

金额单位**百万美元**，分别读取 `transaction_today_amt`、`transaction_mtd_amt`、`transaction_fytd_amt`，保留官方日值、月累计、财年累计。美国财年10月开始，累计值不跨期相加、不由日值重算。

`net_cash_issuance` 为同一记录日的官方现金发行减官方现金赎回，三个金额字段各自作差；`net_is_derived=true` 和 `net_formula` 明确计算口径，终端以 `≈` 标记净额。两侧或对应字段缺失时净额为 `null`，不把缺失当作零。原始零值、负值及负净额均保留。

本轮2026-09-30官方现金发行232753、现金赎回175898，净现金发行56855；月累计净额为-14664。它们与IIIA的记账发行、赎回不同，也不等同于同日拍卖面值或财政赤字。

## IIIB调整溯源

`adjustment_public_debt_transactions_cash_basis` 接口提供记账基准、溢价、折价、联邦融资银行、政府账户交易、储蓄证券利息和通胀保值证券调整等行。JSON的 `adjustments` 保留原始 `adj_type`、`adj_type_desc`、日值／月累计／财年累计、请求URL与抓取时间。

`(-)` 是官方调整方向标签，金额本身仍可能为负；如本轮2026-09-30的通胀保值证券调整原值是-6。代码保留该原值和方向标签，不擅自翻转符号，也不把所有调整行相加。IIIB同时有记账基准和调整项，直接求和没有正确含义。

现金总额直接读取表II官方值，**不通过调整项自行拼算**。总额缺失而调整行仍可得时，保留明细、提示总额缺失；总额可得而调整表为空时，保留官方总额并提示调整明细未列示。网络失败、结构或单位错误则整体报错，不将抓取失败伪装为历史缺口。

## 请求、历史与缓存

官方接口：

```text
https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/dts/deposits_withdrawals_operating_cash
https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/dts/adjustment_public_debt_transactions_cash_basis
```

按日期及原始行号排序，每页1000行、每个查询最多300页。校验页数、计数、日期范围、金额单位、行号及跨页重复／顺序；分页过程中总数变化直接报错。新旧现金总额类别分别使用官方 `eq` 条件请求；本轮实测含括号类别的 `in` 请求返回空数组，因此不使用该过滤，也不把其空结果误认为历史不存在。

复用 `provider/fiscaldata.Client` 六小时语义验证缓存，位于 `~/.sec/cache/fiscaldata`，URL及参数参与缓存键。`--refresh` 强制获取，错误响应不会覆盖该请求的有效旧缓存；`--no-cache` 不读写缓存。未来时间戳、损坏缓存和过期响应不复用，失败时不回退过期值。

JSON保留地区、指标、频率、现金口径、单位、日期范围、来源及当前可见版本；逐条记录保留原始字段与抓取时间。`published_at`、`revision_id` 为 `null`，不将抓取时间冒充公布时间，也不承诺历史初值。原始响应另在验证缓存中保存。

## 限制与测试

这些是财政部整体现金流，不提供CUSIP层面的到账确认，也不提供剔除SOMA等持有人分项后的公众净融资。逐券拍卖面值见 [`macro us auctions`](us-auctions.md)，记账交易见 [`macro us debt`](us-debt.md)，三种口径不直接拼接。

真实离线样本覆盖2005-10-03和2026-09-30；现金总额样本仅保留相关两行并相应重设计数，IIIB样本保留全部行，文件均压缩为单行。测试覆盖新旧标签／账户、官方总额与记账基准区分、负调整、零与空值、单侧缺失、调整明细缺失、跨财年累计重置、重复总额、分页截断／变化／重复、单位、缓存及失败刷新、取消、CLI过滤和JSON／终端输出。
