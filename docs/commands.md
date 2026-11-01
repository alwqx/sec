# 命令与 Desktop 功能对照

本文按功能列出 `sec` 的取数命令及其在 Desktop 中的位置。“规划”表示 Desktop 尚未提供，表中位置是后续开发的目标。指标的频率、字段和官方来源见[宏观指标一览](macro-coverage.md)。

一共 86 个取数命令：个股 11 个、IPO 4 个、宏观 61 个、汇率 6 个、黄金 2 个、美债 2 个。`strategy` 基于已有行情计算，`upgrade` 和 `watch add/remove` 不取数，都不计入。

## Desktop 菜单

| 一级菜单 | 对应命令 | 状态 |
| --- | --- | --- |
| 自选与行情 | 个股命令 | 已有，部分功能待补齐 |
| IPO | `ipo *` | 已有，缺 `ipo calendar` |
| 公告中心 | `announcements`、`insider` | 已有，缺增减持 |
| 宏观 | `macro *`、`bond`、`bond-history` | 已有；原“美债收益率”菜单已并入 |
| 汇率 | `fx *` | 规划 |
| 黄金行情 | `metal`、`metal-history` | 已有 |
| 设置 | `upgrade` | 已有 |

## 个股

| 命令 | 数据 | Desktop 位置 | 状态 |
| --- | --- | --- | --- |
| `search` | 按代码、名称或拼音搜索证券 | 自选与行情 → 证券列表 | 已有 |
| `watch` | 自选行情总览 | 自选与行情 → 证券列表 | 部分：只显示名称与代码，缺行情 |
| `quote` | 单只／多只证券实时行情 | 自选与行情 → 概览 | 部分：只显示选中证券 |
| `quote-history` | 历史行情 | 自选与行情 → 概览 | 已有 |
| `kline` | 蜡烛图，支持 MA、布林带叠加 | 自选与行情 → 概览 | 部分：缺指标叠加 |
| `info` | 公司资料、核心指标与分红 | 自选与行情 → 公司、分红 | 已有 |
| `financials` | 资产负债表、利润表、现金流量表 | 自选与行情 → 财报 | 已有 |
| `annual-report` | 年报 PDF 下载 | 自选与行情 → 财报 | 已有，Desktop 另含半年报和季报 |
| `valuation` | 估值指标与 DCF | 自选与行情 | 部分：只有 PE、PB |
| `strategy ma / macd / rsi / boll` | 技术分析信号（计算） | 自选与行情 | 规划 |

## 公告与 IPO

| 命令 | 数据 | Desktop 位置 | 状态 |
| --- | --- | --- | --- |
| `announcements` | 公司公告 | 公告中心；个股的公告标签页 | 已有 |
| `insider` | 按“增持／减持”标题筛选的公告，不是结构化交易记录 | 公告中心，名称为“增减持公告” | 规划 |
| `ipo list` | 新股上市列表 | IPO | 已有 |
| `ipo prospectus`、`ipo download` | 招股说明书查询与下载 | IPO | 已有 |
| `ipo calendar` | IPO 相关公告日期，不是准确的申购日历 | IPO，名称为“发行公告日期” | 规划 |

## 宏观

全部已有。页面先切换中国／美国（与 `macro cn`、`macro us` 一致），再按类别选择指标；分类来自 `cmd/macro/catalog.go`，`--help` 与 Desktop 菜单共用。

| 类别 | `macro cn` | `macro us` |
| --- | --- | --- |
| 增长与产出 | `gdp`、`industrial`、`pmi`、`nonmanufacturing-pmi` | `gdp`、`industrial-production`、`capacity-utilization` |
| 价格 | `cpi`、`core-cpi`、`ppi` | `cpi`、`core-cpi`、`pce`、`core-pce` |
| 就业 | `unemployment` | `payrolls`、`unemployment`、`participation`、`earnings`、`weekly-hours`、`initial-claims`、`continued-claims` |
| 消费与收入 | `retail`、`household` | `real-pce`、`real-income` |
| 投资与房地产 | `investment`、`property` | `equipment-investment`、`housing-starts`、`housing`、`house-price` |
| 企业 | `enterprise` | `corporate-profits`、`business-inventories`、`new-orders` |
| 货币与信贷 | `money`、`financing`、`loans`、`deposits` | `money`、`bank-credit`、`lending-standards`、`debt-service` |
| 利率与收益率 | `lpr`、`reverse-repo`、`reverse-repo-history`、`rrr`、`treasury` | `target-rate`、`effr`、`treasury` |
| 财政与国债 | `fiscal` | `fiscal`、`debt`、`debt-cash`、`auctions` |
| 对外贸易 | `trade`、`services-trade` | `trade`、`real-trade` |

一个页面可以对应多个命令，不必一个命令一个菜单项。例如 `cpi` 和 `core-cpi` 可在同一页切换；`reverse-repo` 和 `reverse-repo-history` 是不同机制下的两段数据，可放在同一页，但不拼接成一条序列。

中美两边相近的命令不一定是同一口径。`macro cn investment` 是固定资产投资，`macro us equipment-investment` 是私人非住宅设备投资；`macro cn loans` 与 `macro us bank-credit` 的统计范围也不同。

## 汇率

全部为规划。

| 命令 | 数据 |
| --- | --- |
| `fx quote` | 新浪最新参考行情 |
| `fx history` | 新浪、FRED 来源的日线 |
| `fx fixing` | 人民币中间价 |
| `fx reference` | 在岸固定时点参考汇率 |
| `fx cross` | 交叉汇率（计算） |
| `fx spread` | 在岸／离岸报价价差（计算） |

## 黄金与美债

| 命令 | 数据 | Desktop 位置 | 状态 |
| --- | --- | --- | --- |
| `metal`、`metal-history` | 黄金日行情与历史 | 黄金行情 | 已有 |
| `bond`、`bond-history` | 美债收益率与历史，显示五个期限及 10 年期变动 | 宏观 → 美国 → 利率与收益率 → 美债收益率 | 已有 |
| `macro us treasury` | 美债收益率全部期限及统一导出 | 宏观 → 美国 → 利率与收益率 → 美债收益率 | 已有 |

## 命令改名

改名后旧名继续可用，只是不再出现在命令列表中。

| 现名 | 旧名 | 原因 |
| --- | --- | --- |
| `financials` | `balance-sheet`、`bs` | 查询三类财报，不只是资产负债表 |
| `annual-report` | `balance-sheet-download`、`bsd` | 下载的是年报 PDF |
| `macro us housing-starts` | `macro us investment` | 数据是住宅开工和建筑许可，与 `macro cn investment` 同名但不是一回事 |
