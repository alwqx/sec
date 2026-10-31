# SEC

SEC（Security Information Client）提供 CLI 与 Desktop 两种界面，用于查询证券行情、公司资料、财务报表、公告、IPO、黄金与美债收益率。两端复用数据源与公共业务能力，并共享同一份自选证券列表。

[下载安装](https://github.com/alwqx/sec/releases) · [Desktop 截图](#desktop-截图) · [CLI 命令](#cli-命令) · [开发计划](#开发计划)

## 功能概览

| 功能           | Desktop                                                                   | CLI                                           |
| -------------- | ------------------------------------------------------------------------- | --------------------------------------------- |
| 证券搜索与自选 | 名称／代码／拼音搜索，自选增删，与 CLI 共享存储                           | `search`、`watch`                             |
| 实时与历史行情 | 行情自动刷新，日 K 线、成交量与成交额，日期范围、快捷区间和复权选择       | `quote`、`quote-history`、`kline`             |
| 公司研究       | 公司资料、行业、PE、PB、总市值、流通市值与分红记录                        | `info`                                        |
| 财务报表       | 资产负债表、利润表、现金流量表，报告期筛选，CSV／JSON 导出                | `balance-sheet`                               |
| 报告原文       | 年报、半年报、一季报、三季报，年份范围查询、单份／多选 PDF 下载及任务进度 | `balance-sheet-download`（年报）              |
| 公告           | 标题搜索、报告类别筛选、分页与原文查看                                    | `announcements`、`insider`（增减持公告）      |
| IPO            | 新股上市日期筛选，招股说明书查询、原文查看及单份／多选下载                | `ipo list / calendar / prospectus / download` |
| 黄金           | Au99.99 最新日行情、历史蜡烛图与日期筛选                                  | `metal`、`metal-history`                      |
| 美债           | 11 个期限收益率、收益率曲线、历史趋势与明细                               | `bond`、`bond-history`                        |
| 技术分析与估值 | 待接入，见开发计划                                                        | `strategy`、`valuation`                       |
| 版本与更新     | 设置页版本、构建时间、提交号，手动检查更新和 Release 下载入口             | `--version`、`upgrade`                        |

市场覆盖按功能区分：Desktop 搜索与实时行情支持 A 股、港股和美股；证券历史 K 线支持沪深和港股；公司资料、核心指标、财报、分红及个股公告面向沪深北 A 股。黄金为上海黄金交易所 Au99.99 日行情。

## 安装

### Desktop（macOS Apple Silicon）

1. 打开 [Releases](https://github.com/alwqx/sec/releases)，下载 `sec-<版本>-desktop-darwin-arm64.dmg`，例如 `sec-v0.3.16-desktop-darwin-arm64.dmg`（以实际发布资源为准）。
2. 打开 DMG，将应用拖入 `Applications`，启动 SEC。
3. 在「自选与行情」中搜索证券并加入自选；在「设置」中调整外观、行情刷新间隔和下载目录。

当前 Release 仅提供 Apple Silicon 的 Desktop 安装包，Intel Mac 与 Windows Desktop 安装包尚未提供。macOS 安装包采用 ad-hoc 签名，尚未完成 Developer ID 签名与 Apple 公证；系统拦截时的说明见 [Desktop 构建与发布文档](docs/desktop.md#发布macos)。

Desktop 支持浅色、深色及跟随系统外观。导出文件与 PDF 默认保存到 `~/Downloads`，可在设置中修改；同名文件自动追加编号，避免覆盖。检查更新后通过官方 Release 下载并手动安装新版。

### CLI

从 [Releases](https://github.com/alwqx/sec/releases) 下载对应平台的 CLI 压缩包并解压：Linux／macOS 支持 amd64、arm64，Windows 支持 amd64。将 `sec`（Windows 为 `sec.exe`）所在目录加入 `PATH`。

```shell
sec --version
sec --help
sec search 招商银行
sec quote SH600036
```

CLI 命令名和可执行文件名保持小写 `sec`。

## Desktop 截图

以下为当前 Desktop 前端界面，使用演示数据生成；证券列表、行情和报表数值不代表实时数据。

### 自选与行情

侧栏切换工作空间，证券列表与研究区并列展示；个股研究包含概览、公司、财报、分红与公告。K 线支持日期范围、复权选择、成交量副图和逐日量额查看。

![SEC Desktop 浅色主题：自选证券、核心指标与历史 K 线](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-quote.3rc18xtfai.webp)

### 财报研究

切换三类财务报表与报告期，导出 CSV／JSON；「定期报告」提供四类报告原文查询与 PDF 下载。

![SEC Desktop 财报页：报告期筛选、财务数据与导出入口](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-financials.4g5asyp8t7.webp)

### IPO

「新股上市」按上市日期筛选，并可进入证券研究或查询招股说明书；「招股说明书」支持披露日期筛选、原文查看与多选下载。

![SEC Desktop IPO 页：新股上市日期筛选与招股书入口](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-ipo.9rk7doetna.webp)

### 黄金行情与美债收益率

![SEC Desktop 黄金行情：最新日行情、日期筛选与蜡烛图](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-metal.39lzkd3udz.webp)

![SEC Desktop 美债收益率：各期限收益率与收益率曲线](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-bond.1vzggbssd1.webp)

## CLI 与 Desktop 共享自选

CLI `sec watch` 与 Desktop「我的自选」统一使用 `~/.sec/watchlist.json`。两端通过公共 `watchlist` 包执行查询、添加、删除，按证券标识（含市场前缀、不区分大小写）去重，新增最多 500 只。Desktop 偏好仍保存在 `~/.sec/desktop/workspace.json`，保存偏好不会替换自选列表。

首次使用新版 CLI 或 Desktop 时，自动合并原 CLI 列表与 Desktop 自选，保留证券类型、交易所和已有添加时间；有旧数据的源文件先生成同目录 `*.pre-shared-*.bak` 备份，再写入带版本的共享文档并移除 Desktop 旧自选字段。迁移失败时返回错误，不用空列表覆盖损坏数据。两端应使用支持共享格式的新版程序；旧版 CLI 不支持新的版本化文档。

跨进程文件锁与同目录临时文件原子替换保护增删操作。Desktop 返回「自选与行情」、窗口重新获得焦点或点击「刷新自选」时读取最新共享列表。迁移时不截断超过 500 只的历史列表，此时可删除但不能继续新增。

## CLI 命令

| 命令                                          | 用途                                     |
| --------------------------------------------- | ---------------------------------------- |
| `search`                                      | 按代码、名称或拼音搜索证券               |
| `quote`、`quote-history`                      | 单只／多只证券实时行情、历史行情         |
| `kline`                                       | 终端蜡烛图，支持 MA、布林带叠加          |
| `info`                                        | 公司资料、核心指标与分红                 |
| `watch`、`watch add`、`watch remove`          | 自选行情总览及增删                       |
| `balance-sheet`                               | 三类财务报表查询及 CSV／JSON 导出        |
| `balance-sheet-download`                      | 按单年或年份范围下载年报 PDF             |
| `announcements`、`insider`                    | 公司公告、增减持公告                     |
| `ipo list / calendar / prospectus / download` | 新股列表、IPO 公告日历、招股书查询与下载 |
| `metal`、`metal-history`                      | 黄金日行情与历史                         |
| `fx quote`、`fx history`                     | 在岸／离岸人民币、美元及港币参考行情、来源独立的新浪／FRED日线，见 [汇率文档](docs/forex.md) |
| `fx fixing`、`fx reference`                  | 人民币中间价、在岸固定时点参考汇率，保留各自价格和时间口径 |
| `fx cross`、`fx spread`                      | 按同源同口径计算交叉汇率、在岸／离岸报价价差，保留公式、输入及时间对齐状态 |
| `bond`、`bond-history`                        | 美债收益率与历史                         |
| `macro cn cpi`、`macro cn core-cpi`、`macro cn ppi` | 国家统计局月度价格指数，见 [NBS 文档](docs/nbs.md) |
| `macro cn pmi`                                | 国家统计局制造业 PMI 及生产、订单、出口订单、就业分项 |
| `macro cn nonmanufacturing-pmi` / `macro cn npmi` | 非制造业、服务业、建筑业商务活动指数；`--details` 展示九个全国分项 |
| `macro cn industrial` / `macro cn ip`          | 规模以上工业增加值单月同比、累计同比及可检索的季调环比 |
| `macro cn retail`                             | 社会消费品零售总额当月金额、同比、累计金额及累计同比 |
| `macro cn investment`                         | 固定资产投资总量及制造业、基建、房地产分项的累计口径，见 [投资文档](docs/cn-investment.md) |
| `macro cn property`                           | 新建商品房销售面积、销售额及房屋新开工面积，见 [房地产文档](docs/cn-property.md) |
| `macro cn enterprise`                         | 规模以上工业企业利润、总存货/产成品存货及应收账款，区分累计流量与期末余额，见 [工业企业文档](docs/cn-enterprise.md) |
| `macro cn household` | 全国居民人均累计收入、消费支出及官方实际同比，见 [居民收支文档](docs/cn-household.md) |
| `macro cn deposits` | 住户人民币存款余额、严格基期计算同比和报告新增额，见 [居民存款文档](docs/cn-deposits.md) |
| `macro cn fiscal` | 全国一般公共预算、政府性基金累计收支及官方同比，见 [中国财政文档](docs/cn-fiscal.md) |
| `macro cn trade` | 海关货物进出口人民币/美元月度总量；支持 `--partners-currency cny` 查询人民币伙伴月值及累计值，见 [货物贸易文档](docs/cn-trade.md) |
| `macro cn services-trade` | 服务贸易人民币/美元月度进出口与官方差额，保留历史缺口，见 [服务贸易文档](docs/cn-services-trade.md) |
| `macro cn unemployment`                       | 全国城镇调查失业率；`--age-groups` 展示年龄分组并标记口径变化 |
| `macro cn financing` / `macro cn afre`         | 人民银行社融存量、同比、月度/累计增量；`--details` 展示融资结构，见 [PBC 文档](docs/pbc.md) |
| `macro cn loans`                              | 金融机构人民币贷款余额、同比和新增；`--details` 展示住户／企事业单位期限结构，近似计算值带 `≈` |
| `macro cn money`                              | 人民银行 M1、M2 余额及同比；标注 2025 年 M1 口径修订 |
| `macro cn reverse-repo`                       | 七天逆回购政策利率调整公告，含公布/生效日期及前后值；覆盖自 2024-07-22 起 |
| `macro cn reverse-repo-history`               | 固定利率机制切换前七天逆回购逐笔中标利率，2015-01-22 至 2024-07-19；与政策调整事件序列分开 |
| `macro cn lpr`                                | 1 年期、5 年期以上 LPR 月度报价、实际报价日、公布时间及基点变化；覆盖自 2019-08 起 |
| `macro cn rrr`                                | 人民币存款准备金率调整，内置 2018 年起可逐条核验历史并增量检查新公告；区分全面/定向及分批生效 |
| `macro cn treasury`                           | 中债国债到期收益率曲线，含 3 个月、6 个月及 1、2、5、10 年期；直接查询指定日期范围，见 [中债文档](docs/chinabond.md) |
| `macro cn gdp`                                | 国家统计局季度实际 GDP 同比、累计同比、季调环比及分段基期的不变价金额，见 [NBS 文档](docs/nbs.md#实际-gdp) |
| `macro us cpi`、`macro us core-cpi`             | BLS美国CPI/核心CPI季调与非季调指数、非季调同比及季调环比；保留缺口与脚注，见 [BLS 文档](docs/bls.md) |
| `macro us pce`、`macro us core-pce`             | BEA经FRED转接的PCE/核心PCE季调链式价格指数、同比与月环比；区别于消费支出金额和实际消费量，见 [FRED 文档](docs/fred.md) |
| `macro us payrolls`                             | BLS季调非农工资岗位总数、月度变化及本机观测版本，见 [BLS 文档](docs/bls.md) |
| `macro us earnings`、`macro us weekly-hours`    | BLS私人非农全部雇员季调平均时薪、平均每周工时、月度变化和同比；保留基期与脚注，见 [BLS 文档](docs/bls.md#平均时薪与每周工时) |
| `macro us unemployment`、`macro us participation` | BLS CPS季调失业率、劳动参与率及百分点变化；保留缺口和修订脚注，见 [BLS 文档](docs/bls.md#失业率与劳动参与率) |
| `macro us initial-claims`、`macro us continued-claims` | 劳工部ETA经FRED转接的季调初请/续请数量、周变化及四周计算均值；按各自周六所属周对齐，见 [FRED 文档](docs/fred.md#初请与续请失业金) |
| `macro us real-pce`                           | BEA经FRED转接的实际消费总量、商品及服务分项；保留季调折年水平与未年化增速，见 [FRED 文档](docs/fred.md#实际个人消费支出) |
| `macro us real-income`                        | BEA经FRED转接的实际可支配个人收入总量、同比与月环比；保留2017年链式美元季调折年水平，见 [实际收入文档](docs/real-income.md) |
| `macro us industrial-production`、`macro us capacity-utilization` | 美联储经FRED转接的工业生产指数与产能利用率；`--sector total\|manufacturing` 选择总量或NAICS制造业，见 [工业生产文档](docs/us-production.md) |
| `macro us effr`                               | 纽约联储有效联邦基金利率、所属日期及前一可用观测值的基点变化，见 [纽约联储文档](docs/nyfed.md) |
| `macro us target-rate`                        | 联邦基金目标利率区间及调整历史；区分政策公布与生效信息，见 [美联储政策利率文档](docs/federalreserve.md) |
| `macro us gdp`                                | 美国实际GDP季度水平、同比及环比年化增速；`--vintages` 查询BEA官方初值与修订增速档案，见 [美国GDP文档](docs/us-gdp.md) |
| `macro us money`、`macro us m2`                | 美国M2月度季调余额、同比与月环比，见 [美国货币供应文档](docs/us-money.md) |
| `macro cn money --standard-json`、`macro us money --standard-json` | 货币指标统一JSON导出：统计口径、来源、缺失状态和原provider结果，见 [统一导出文档](docs/macro-standard.md) |
| `macro cn retail --standard-json`、`macro cn property --standard-json` | 社零、房地产统一JSON导出：单月／累计所属期、官方同比、历史范围及来源序列，见 [统一导出文档](docs/macro-standard.md) |
| `macro us gdp --standard-json`、`macro us effr --standard-json` | 季度GDP、日度EFFR统一JSON导出：折年口径、严格基期、交易日和方法边界，见 [统一导出文档](docs/macro-standard.md) |
| `macro us cpi --standard-json`、`macro us core-cpi --standard-json` | CPI指数与增速统一JSON导出：季调状态、指数基期及严格计算基期，见 [统一导出文档](docs/macro-standard.md) |
| `macro us initial-claims --standard-json`、`macro us continued-claims --standard-json` | 周度申领统一JSON导出：所属周、前周变化、四周均值和输入窗口，见 [统一导出文档](docs/macro-standard.md) |
| `macro us pce --standard-json`、`macro us core-pce --standard-json`、`macro us real-pce --standard-json` | PCE价格与实际消费统一JSON导出：指数／金额、折年口径和分项不可加性，见 [统一导出文档](docs/macro-standard.md) |
| `macro us real-income --standard-json`、`macro us industrial-production --standard-json`、`macro us capacity-utilization --standard-json` | 实际收入与工业统一JSON导出：总体范围、折年口径和百分比／百分点变化，见 [统一导出文档](docs/macro-standard.md) |
| `macro us unemployment --standard-json`、`macro us participation --standard-json`、`macro us earnings --standard-json`、`macro us weekly-hours --standard-json` | 劳动力比率、时薪工时统一JSON导出：CPS／CES范围、分子分母、变化单位与初步标记，见 [统一导出文档](docs/macro-standard.md) |
| `macro us trade`、`macro us real-trade`        | 月度名义贸易与季度实际贸易，出口、进口及官方差额；支持商品/服务分项，见 [美国进出口文档](docs/us-trade.md) |
| `macro us investment`、`macro us equipment-investment` | 住宅开工、建筑许可及季度设备投资，分别保留频率、单位和季调折年口径，见 [美国投资文档](docs/us-investment.md) |
| `macro us bank-credit`、`macro us lending-standards`、`macro us debt-service` | 月平均银行信贷、季度贷款标准及家庭偿债负担，区分百分比和百分点变化，见 [美国信用文档](docs/us-credit.md) |
| `macro us fiscal` | 联邦月度收入、支出、官方盈余/赤字及计算财年累计，见 [美国财政文档](docs/us-fiscal.md) |
| `macro us debt` | 财政部每日可流通／非流通国债记账发行、赎回及计算净额，保留调整与累计值，见 [美国国债交易文档](docs/us-debt.md) |
| `macro us debt-cash` | 国债官方现金发行、现金赎回、计算净现金流及IIIB调整溯源，见 [债务现金流文档](docs/us-debt-cash.md) |
| `macro us auctions` | 国债逐券发行日期、拍卖面值与分配、SOMA及再发行事件，见 [美国拍卖文档](docs/us-auctions.md) |
| `macro us corporate-profits`、`macro us business-inventories`、`macro us new-orders` | 企业税前利润、商业库存及制造业新订单，分开保留季度流量、月末余额和月度流量，见 [美国企业文档](docs/us-enterprise.md) |
| `macro us housing`、`macro us house-price`     | 新房与成屋销售、FHFA季度房价指数，保留各自历史覆盖和统计口径，见 [美国房地产文档](docs/us-housing.md) |
| `strategy ma / macd / rsi / boll`             | 技术指标与信号                           |
| `valuation`                                   | 估值指标与 DCF 分析                      |
| `upgrade`、`--version`                        | CLI 升级与版本信息                       |

使用 `sec <命令> --help` 查看参数，IPO 等子命令使用 `sec ipo <子命令> --help`。

```shell
# 自选与 Desktop 共享
sec watch add SH600036 SZ002475
sec watch

# 财报查询与导出
sec balance-sheet SH600036 --type balance --period annual --output balance.csv
sec balance-sheet-download SH600036 --start-year 2023 --end-year 2025 --output-dir ./reports

# 新股上市日期范围、招股书查询
sec ipo list --since 2026-01-01 --until 2026-06-30
sec ipo prospectus 300750

# 技术分析与估值
sec strategy macd SH600036
sec valuation SH600036
```

以下命令输出为历史示例。

### 搜索证券

```shell
$ sec search lxzk
证券代码 	证券名称 	证券类型 	交易所
SH688047	龙芯中科	stock   	sh
SZ300112	万讯自控	stock   	sz
HK02186 	绿叶制药	stock   	hk
```

### 查看 K 线

```shell
sec kline SH688047 --begin 20260301
```

![](https://github.com/alwqx/picx-images-hosting/raw/master/blog/2026/sec-kline.8vnjmsfq85.webp)

### 查看美债 10 年收益率

```shell
$ sec bond
  日期       | 1 个月 | 3 个月 | 6 个月 | 5 年   | 10 年  | 前值  | 变动 (BP)
  2026-05-18 | 3.69% | 3.68% | 3.77% | 4.27% | 4.61% | 4.59% | +2.0

$ sec bond-history
  日期       | 1 个月 | 3 个月 | 6 个月 | 5 年   | 10 年  | 变动 (BP)
  2026-04-20 | 3.69% | 3.71% | 3.72% | 3.86% | 4.26% | +0.0
  2026-04-21 | 3.67% | 3.69% | 3.73% | 3.91% | 4.30% | +4.0
  2026-04-22 | 3.68% | 3.69% | 3.72% | 3.91% | 4.30% | +0.0
  2026-04-23 | 3.69% | 3.69% | 3.72% | 3.96% | 4.34% | +4.0
  2026-04-24 | 3.69% | 3.69% | 3.71% | 3.92% | 4.31% | -3.0
  2026-04-27 | 3.70% | 3.68% | 3.72% | 3.94% | 4.35% | +4.0
  2026-04-28 | 3.68% | 3.68% | 3.72% | 3.97% | 4.36% | +1.0
  2026-04-29 | 3.68% | 3.68% | 3.73% | 4.05% | 4.42% | +6.0
  2026-04-30 | 3.72% | 3.68% | 3.71% | 4.02% | 4.40% | -2.0
  2026-05-01 | 3.71% | 3.68% | 3.71% | 4.02% | 4.39% | -1.0
  2026-05-04 | 3.71% | 3.70% | 3.76% | 4.08% | 4.45% | +6.0
  2026-05-05 | 3.70% | 3.69% | 3.75% | 4.08% | 4.43% | -2.0
  2026-05-06 | 3.70% | 3.69% | 3.74% | 3.99% | 4.36% | -7.0
  2026-05-07 | 3.72% | 3.69% | 3.74% | 4.04% | 4.41% | +5.0
  2026-05-08 | 3.71% | 3.69% | 3.74% | 4.02% | 4.38% | -3.0
  2026-05-11 | 3.71% | 3.70% | 3.77% | 4.07% | 4.42% | +4.0
  2026-05-12 | 3.71% | 3.70% | 3.77% | 4.12% | 4.46% | +4.0
  2026-05-13 | 3.71% | 3.69% | 3.77% | 4.12% | 4.46% | +0.0
  2026-05-14 | 3.72% | 3.69% | 3.76% | 4.13% | 4.47% | +1.0
  2026-05-15 | 3.71% | 3.69% | 3.77% | 4.26% | 4.59% | +12.0
  2026-05-18 | 3.69% | 3.68% | 3.77% | 4.27% | 4.61% | +2.0
```

### 查看黄金价格走势

```shell
$ sec metal
  日期       | 名称    | 收盘                 | 开盘    | 最高 | 最低
  2026-05-18 | Au99.99 | 999.69 -6.32 -0.628% | 1000.01 | 1005 | 980.02

$ sec metal-history
  日期       | 名称    | 收盘                 | 开盘    | 最高    | 最低
  2026-04-20 | Au99.99 | 1054 0.98 0.0931%    | 1055    | 1070    | 1045
  2026-04-21 | Au99.99 | 1050.5 -3.5 -0.332%  | 1055.5  | 1059    | 1048.06
  2026-04-22 | Au99.99 | 1049.1 -1.37 -0.13%  | 1053    | 1055.8  | 1038.88
  2026-04-23 | Au99.99 | 1037.5 -11.61 -1.11% | 1047    | 1053    | 1034.5
  2026-04-24 | Au99.99 | 1033.2 -4.25 -0.41%  | 1040    | 1044    | 1028
  2026-04-27 | Au99.99 | 1037.2 3.96 0.383%   | 1039.9  | 1044    | 1033
  2026-04-28 | Au99.99 | 1020.7 -16.48 -1.59% | 1035    | 1038.9  | 1019
  2026-04-29 | Au99.99 | 1009.9 -10.85 -1.06% | 1007    | 1020    | 1006.32
  2026-04-30 | Au99.99 | 1013.6 3.72 0.368%   | 1007    | 1016    | 995
  2026-05-06 | Au99.99 | 1026.9 13.34 1.32%   | 1017    | 1027    | 1014.04
  2026-05-07 | Au99.99 | 1038.9 12 1.17%      | 1030    | 1044.99 | 1025.1
  2026-05-08 | Au99.99 | 1032 -6.94 -0.668%   | 1038.2  | 1045    | 1031
  2026-05-11 | Au99.99 | 1025.8 -6.18 -0.599% | 1033.5  | 1039.9  | 1018.31
  2026-05-12 | Au99.99 | 1028.5 2.69 0.262%   | 1025    | 1040.1  | 1025
  2026-05-13 | Au99.99 | 1030 1.48 0.144%     | 1030    | 1032.9  | 1018
  2026-05-14 | Au99.99 | 1028.7 -1.31 -0.127% | 1030.8  | 1031.2  | 1022.02
  2026-05-15 | Au99.99 | 1006 -22.67 -2.2%    | 1031.5  | 1031.5  | 1000.05
  2026-05-18 | Au99.99 | 999.69 -6.32 -0.628% | 1000.01 | 1005    | 980.02
```

### 查看证券基本信息

```shell
$ sec info lxjm -d
证券代码	SZ002475
简称历史	立讯精密
公司名称	立讯精密工业股份有限公司
上市日期	2010-09-15
发行价格	28.80
行业分类	消费电子
主营业务	公司专注于连接器的研发、生产和销售，产品主要应用于 3C（电脑、通讯、消费电子）、汽车和通讯设备等领域。
办公地址	广东省东莞市清溪镇北环路 313 号
公司网址	http://www.luxshare-ict.com
当前价格	35.77
市净率 PB	3.60
市盈率 TTM	85.22
总市值  	2593.82 亿
流通市值	2589.12 亿

公告日期   	分红送配           	除权除息日 	股权登记日
2025-04-26	10 派 2.00 元        	--        	--
2024-07-05	10 派 2.99 元        	2024-07-11	2024-07-10
2023-06-09	10 派 1.30 元        	2023-06-15	2023-06-14
2022-07-07	10 派 1.10 元        	2022-07-13	2022-07-12
2021-06-30	10 派 1.10 元        	2021-07-08	2021-07-07
2020-06-10	10 转 3.00 股派 1.20 元	2020-06-17	2020-06-16
2019-06-28	10 转 3.00 股派 0.50 元	2019-07-05	2019-07-04
2018-07-10	10 转 3.00 股派 0.60 元	2018-07-17	2018-07-16
2017-06-28	10 转 5.00 股派 0.80 元	2017-07-06	2017-07-05
2016-06-03	10 转 5.00 股派 0.90 元	2016-06-13	2016-06-08
2015-06-12	10 转 5.00 股派 0.80 元	2015-06-19	2015-06-18
2014-05-15	10 转 4.00 股派 0.70 元	2014-05-21	2014-05-20
2013-06-15	10 转 5.00 股派 1.00 元	2013-06-21	2013-06-20
2012-06-02	10 转 4.00 股派 2.00 元	2012-06-08	2012-06-07
2011-05-20	10 转 5.00 股派 3.00 元	2011-05-26	2011-05-25
```

### 查看行情信息

```shell
$ sec quote lxzk
时间                	当前价格   	昨收  	今开 	最高   	最低  	成交量   	成交额 	名称     	证券代码
2024-09-30 15:00:01	119.62 20%	99.68	106 	119.62	104.5	825.67 万	9.38 亿	龙芯中科	SH688047
$ sec quote lxzk,lxjm,SH600036
时间                	当前价格   	昨收  	今开  	最高   	最低  	成交量   	成交额  	名称     	证券代码
2024-09-30 15:00:00	43.46 7.4%	40.48	42   	43.95 	41.01	1.60 亿  	68.42 亿	立讯精密	SZ002475
2024-09-30 15:00:01	119.62 20%	99.68	106  	119.62	104.5	825.67 万	9.38 亿 	龙芯中科	SH688047
2024-09-30 15:00:00	37.61 5.6%	35.63	36.35	38    	35.92	2.56 亿  	94.43 亿	招商银行	SH600036
```

### 查看行情历史

```shell
$ sec quote-history lxjm --begin 20250601 --end 20250630
日期       | 名称     | 收盘               | 开盘  | 最高  | 最低  | 成交额  | 成交量   | 振幅 | 换手率 | 证券代码
2025-06-03 | 立讯精密 | 30.32 -0.08 -0.26% | 30.19 | 30.46 | 30.12 | 17.89 亿 | 59.00 万  | 1.12 | 0.82   | SZ002475
2025-06-04 | 立讯精密 | 30.8 0.48 1.6%     | 30.38 | 30.98 | 30.3  | 25.79 亿 | 83.92 万  | 2.24 | 1.16   | SZ002475
2025-06-05 | 立讯精密 | 31.63 0.83 2.7%    | 30.85 | 31.86 | 30.83 | 44.55 亿 | 142.09 万 | 3.34 | 1.96   | SZ002475
2025-06-06 | 立讯精密 | 31.32 -0.31 -0.98% | 31.41 | 31.55 | 31.17 | 26.73 亿 | 85.34 万  | 1.2  | 1.18   | SZ002475
2025-06-09 | 立讯精密 | 31.93 0.61 1.9%    | 31.7  | 32.08 | 31.53 | 30.58 亿 | 96.01 万  | 1.76 | 1.33   | SZ002475
2025-06-10 | 立讯精密 | 31.78 -0.15 -0.47% | 31.79 | 32.2  | 31.35 | 38.11 亿 | 119.82 万 | 2.66 | 1.66   | SZ002475
2025-06-11 | 立讯精密 | 31.97 0.19 0.6%    | 31.9  | 32.54 | 31.8  | 31.79 亿 | 98.95 万  | 2.33 | 1.37   | SZ002475
2025-06-12 | 立讯精密 | 31.84 -0.13 -0.41% | 31.65 | 32.16 | 31.55 | 22.92 亿 | 71.83 万  | 1.91 | 0.99   | SZ002475
2025-06-13 | 立讯精密 | 31.25 -0.59 -1.9%  | 31.6  | 31.98 | 31.2  | 25.17 亿 | 80.06 万  | 2.45 | 1.11   | SZ002475
2025-06-16 | 立讯精密 | 31.91 0.66 2.1%    | 31.05 | 32.05 | 31.02 | 30.52 亿 | 96.10 万  | 3.3  | 1.33   | SZ002475
2025-06-17 | 立讯精密 | 32.67 0.76 2.4%    | 31.91 | 32.8  | 31.83 | 49.98 亿 | 153.61 万 | 3.04 | 2.12   | SZ002475
2025-06-18 | 立讯精密 | 32.94 0.27 0.83%   | 32.41 | 33.05 | 32.4  | 38.87 亿 | 118.32 万 | 1.99 | 1.64   | SZ002475
2025-06-19 | 立讯精密 | 32.72 -0.22 -0.67% | 32.96 | 33.34 | 32.62 | 34.97 亿 | 106.17 万 | 2.19 | 1.47   | SZ002475
2025-06-20 | 立讯精密 | 33.21 0.49 1.5%    | 32.65 | 33.49 | 32.59 | 39.50 亿 | 119.14 万 | 2.75 | 1.65   | SZ002475
2025-06-23 | 立讯精密 | 33.19 -0.02 -0.06% | 32.99 | 33.77 | 32.88 | 31.22 亿 | 93.65 万  | 2.68 | 1.29   | SZ002475
2025-06-24 | 立讯精密 | 33.23 0.04 0.12%   | 33.3  | 33.56 | 33.01 | 33.37 亿 | 100.54 万 | 1.66 | 1.39   | SZ002475
2025-06-25 | 立讯精密 | 33.24 0.01 0.03%   | 33.26 | 33.28 | 32.77 | 34.61 亿 | 104.90 万 | 1.53 | 1.45   | SZ002475
2025-06-26 | 立讯精密 | 32.97 -0.27 -0.81% | 33.24 | 33.55 | 32.95 | 27.86 亿 | 83.77 万  | 1.81 | 1.16   | SZ002475
2025-06-27 | 立讯精密 | 33.87 0.9 2.7%     | 32.98 | 34.1  | 32.69 | 52.18 亿 | 154.94 万 | 4.28 | 2.14   | SZ002475
2025-06-30 | 立讯精密 | 34.69 0.82 2.4%    | 33.95 | 34.78 | 33.95 | 43.77 亿 | 126.69 万 | 2.45 | 1.75   | SZ002475
```

## 本地开发

CLI 使用 Go 1.25 或更新版本：

```shell
go build -o sec .
go test -v ./...
```

macOS Desktop 还需要 Node.js（Release 使用 Node 20）、npm 与 Xcode Command Line Tools，先构建前端，再编译桌面端：

```shell
npm ci --prefix desktop/ui
npm run build --prefix desktop/ui
go build -o sec-desktop ./desktop
./sec-desktop
```

开发联调、应用打包与架构说明见 [Desktop 文档](docs/desktop.md)。

## 开发计划

### TODO：共享业务能力

- [ ] 将 `cmd/strategy` 中的 MA、MACD、RSI、布林带等计算提取到 CLI 与 Desktop 共用的业务包，命令层仅处理参数和终端展示，Desktop 不重复实现算法。
- [ ] 将 `cmd/valuation` 中的估值指标与 DCF 等计算提取到公共业务包，统一数据获取、计算口径、参数校验和缺失值处理，再由 CLI／Desktop 分别展示；同步检查“历史 PE”等已有口径。

### TODO：Desktop 功能补齐

已覆盖证券搜索、公司核心指标与分红、基础行情与蜡烛图（含日期范围、快捷区间和复权选择）、三类财报及 CSV/JSON 导出、定期报告（年报／半年报／一季报／三季报）年度范围查询与单份／多选 PDF 下载（含任务进度）、公告查询（含年报／中报／一季报／三季报类别筛选）、IPO 新股上市日期范围筛选、招股说明书及相关 PDF 查询与单份／多选下载、黄金行情、美债收益率及设置页版本详情与更新入口。以下按 CLI 与 Desktop 当前差异整理，优先补齐现有页面中的常用能力。

| 优先级 | CLI 功能                          | Desktop 当前差距                           | 待办                                                     |
| ------ | --------------------------------- | ------------------------------------------ | -------------------------------------------------------- |
| 高     | `watch` 自选行情总览              | 自选列表仅展示名称与代码                   | 增加最新价、涨跌幅及多证券行情表                         |
| 高     | `kline --ma / --boll`             | 蜡烛图没有指标叠加                         | 增加 MA 均线、布林带开关及参数设置                       |
| 中     | `insider`                         | 没有增减持专题入口                         | 个股公告增加增持／减持快捷分类                           |
| 中     | `strategy ma / macd / rsi / boll` | 没有技术分析与信号明细                     | 个股研究增加技术分析，展示指标、副图、信号日期和原因     |
| 中     | `valuation`                       | 已有 PE、PB，缺少完整估值分析              | 增加 PS、PEG、ROE、Graham 等指标，再增加可调整参数的 DCF |
| 中     | `ipo calendar`                    | 当前提供新股上市列表，缺少发行公告日期视图 | 增加独立的 IPO 发行公告日期视图                          |
| 中     | `quote` 多证券行情                | 当前仅展示选中证券行情                     | 支持从自选中选择多只证券，并排比较行情                   |

建议实施顺序：自选行情总览 → MA／布林带，其余功能按上述优先级推进。

移植时需明确以下数据口径：

- `insider` 当前基于“增持／减持”标题查询公告，不是结构化高管交易记录，页面应称为“增减持公告”。
- CLI `ipo calendar` 当前使用 IPO 相关公告日期，不能直接标为准确的申购日历。
- `valuation` 当前的“历史 PE”使用当前价格除以历年 EPS，并非历史时点的真实 PE；移植前需检查并明确计算口径，避免误标为历史估值走势。

### TODO：Desktop 构建警告

- [ ] 升级或修补 `github.com/webview/webview_go` 依赖，消除新版 Clang 的 `-Wdeprecated-literal-operator` 警告：将 `operator"" _cls`、`operator"" _sel`、`operator"" _str` 改为后缀紧邻引号的写法（如 `operator""_cls`）。通过依赖升级或仓库内维护补丁处理，不直接修改 Go 模块缓存。

当前这三条警告来自 WebView 的 C++ 代码，不代表构建失败；未将警告视为错误时，可正常生成二进制。临时隐藏该类警告，可在 `desktop` 目录执行：

```shell
CGO_CXXFLAGS="-O2 -g -Wno-deprecated-literal-operator" go build -o sec-desktop
```

该参数仅隐藏警告，不替代依赖修复。详见 [Clang 诊断说明](https://clang.llvm.org/docs/DiagnosticsReference.html#wdeprecated-literal-operator)。

## 致谢

- [efinance](https://efinance.readthedocs.io/)
- [akshare](https://github.com/akfamily/akshare)
- [rains](https://github.com/rookie0/rains)
