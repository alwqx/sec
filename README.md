# SEC

SEC（Security Information Client）提供 CLI 与 Desktop 两种界面，用于查询证券行情、公司资料、财务报表、公告、IPO、宏观数据、汇率、贵金属、大宗商品与美债收益率。两端复用数据源与公共业务能力，并共享同一份自选证券列表。

[下载安装](https://github.com/alwqx/sec/releases) · [Desktop 截图](#desktop-截图） · [CLI 命令](#cli-命令） · [开发计划](#开发计划)

## 功能概览

| 功能           | Desktop                                                                                                        | CLI                                                 |
| -------------- | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- |
| 证券搜索与自选 | 名称／代码／拼音搜索，自选增删，与 CLI 共享存储                                                                | `search`、`watch`                                   |
| 实时与历史行情 | 行情自动刷新，日 K 线、成交量与成交额，日期范围、快捷区间和复权选择                                            | `quote`、`quote-history`、`kline`                   |
| 公司研究       | 公司资料、行业、PE、PB、总市值、流通市值与分红记录                                                             | `info`                                              |
| 财务报表       | 资产负债表、利润表、现金流量表，报告期筛选，CSV／JSON 导出；Desktop 有利润桑基图、利润和现金瀑布图及多维趋势图 | `financials`                                        |
| 报告原文       | 年报、半年报、一季报、三季报，年份范围查询、单份／多选 PDF 下载及任务进度                                      | `annual-report`（年报）                             |
| 公告           | 标题搜索、报告类别筛选、分页与原文查看；美股为 SEC EDGAR 官方申报                                              | `announcements`、`insider`（增减持公告）、`filings` |
| IPO            | 新股上市日期筛选，招股说明书查询、原文查看及单份／多选下载                                                     | `ipo list / calendar / prospectus / download`       |
| 贵金属         | 上海黄金交易所 Au99.99 黄金、Ag99.99 白银日行情与蜡烛图                                                        | `metal`                                             |
| 大宗商品       | 原油、铜、铁矿石、螺纹钢、豆粕及 WTI、布伦特、COMEX 铜期货行情与日 K 线                                        | `commodity`                                         |
| 美债           | 11 个期限收益率、收益率曲线、历史趋势与明细                                                                    | `bond`、`bond-history`                              |
| 技术分析       | 双均线、MACD、RSI、布林带，参数可调，买卖信号与明细，A 股日线回测                                              | `strategy`                                          |
| 估值           | PE、PB、PS 历史分位和同行业对比，股息率，PEG、ROE、格雷厄姆数、规则汇总，可调参数的 DCF                        | `valuation`                                         |
| 版本与更新     | 设置页版本、构建时间、提交号，手动检查更新和 Release 下载入口                                                  | `--version`、`upgrade`                              |

市场覆盖按功能区分：Desktop 搜索与实时行情支持 A 股、港股和美股；证券历史 K 线支持沪深、港股和美股；美股的 `sec info` 公司资料来自 SEC EDGAR；公司资料、核心指标、财报和分红面向沪深北 A 股，美股的公司资料、财报和分红（`sec info -d`）来自 SEC EDGAR；估值支持沪深北 A 股和美股（SEC XBRL 计算，含股息率）；个股公告支持沪深北 A 股（巨潮资讯）和美股（SEC EDGAR 申报）。贵金属为上海黄金交易所 Au99.99、Ag99.99 日行情；大宗商品为新浪期货的国内主力连续合约和国际期货。

## 安装

### Desktop（macOS Apple Silicon）

1. 打开 [Releases](https://github.com/alwqx/sec/releases)，下载 `sec-<版本>-desktop-darwin-arm64.dmg`，例如 `sec-v0.3.16-desktop-darwin-arm64.dmg`（以实际发布资源为准）。
2. 打开 DMG，将应用拖入 `Applications`，启动 SEC。
3. 在「研究」中搜索证券并加入自选，在「自选」中查看全部自选的行情；在「设置」中调整外观、行情刷新间隔和下载目录。

当前 Release 仅提供 Apple Silicon 的 Desktop 安装包，Intel Mac 与 Windows Desktop 安装包尚未提供。macOS 安装包采用 ad-hoc 签名，尚未完成 Developer ID 签名与 Apple 公证；系统拦截时的说明见 [Desktop 构建与发布文档](docs/desktop.md#发布 macos)。

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

### 自选与研究

「自选」整页显示全部自选证券的行情，可勾选对比；「研究」中证券列表与研究区并列展示；个股研究包含概览、行情、财报、分红与公告。K 线支持日期范围、复权选择、成交量副图和逐日量额查看。

![SEC Desktop 浅色主题：自选证券、核心指标与历史 K 线](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-quote.3rc18xtfai.webp)

### 财报研究

切换三类财务报表与报告期，导出 CSV／JSON；「定期报告」提供四类报告原文查询与 PDF 下载。

![SEC Desktop 财报页：报告期筛选、财务数据与导出入口](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-financials.4g5asyp8t7.webp)

### IPO

「新股上市」按上市日期筛选，并可进入证券研究或查询招股说明书；「招股说明书」支持披露日期筛选、原文查看与多选下载。

![SEC Desktop IPO 页：新股上市日期筛选与招股书入口](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-ipo.9rk7doetna.webp)

### 贵金属行情与美债收益率

![SEC Desktop 黄金行情：最新日行情、日期筛选与蜡烛图](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-metal.39lzkd3udz.webp)

![SEC Desktop 美债收益率：各期限收益率与收益率曲线](https://raw.githubusercontent.com/alwqx/picx-images-hosting/refs/heads/master/blog/2026/sec-desktop-bond.1vzggbssd1.webp)

## CLI 与 Desktop 共享自选

CLI `sec watch` 与 Desktop「我的自选」统一使用 `~/.sec/watchlist.json`。两端通过公共 `watchlist` 包执行查询、添加、删除，按证券标识（含市场前缀、不区分大小写）去重，新增最多 500 只。Desktop 偏好仍保存在 `~/.sec/desktop/workspace.json`，保存偏好不会替换自选列表。

首次使用新版 CLI 或 Desktop 时，自动合并原 CLI 列表与 Desktop 自选，保留证券类型、交易所和已有添加时间；有旧数据的源文件先生成同目录 `*.pre-shared-*.bak` 备份，再写入带版本的共享文档并移除 Desktop 旧自选字段。迁移失败时返回错误，不用空列表覆盖损坏数据。两端应使用支持共享格式的新版程序；旧版 CLI 不支持新的版本化文档。

跨进程文件锁与同目录临时文件原子替换保护增删操作。Desktop 打开「自选」或「研究」、窗口重新获得焦点或点击「刷新自选」时读取最新共享列表。迁移时不截断超过 500 只的历史列表，此时可删除但不能继续新增。

## CLI 命令

命令按功能的分类及其在 Desktop 中的位置见 [命令与 Desktop 功能对照](docs/commands.md)。

| 命令                                                                                                                                                                                                                      | 用途                                                                                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `search`                                                                                                                                                                                                                  | 按代码、名称或拼音搜索证券                                                                                                                                    |
| `quote`、`quote-history`                                                                                                                                                                                                  | 单只／多只证券实时行情、历史行情                                                                                                                              |
| `kline`                                                                                                                                                                                                                   | 终端蜡烛图，支持 MA、布林带叠加                                                                                                                               |
| `info`                                                                                                                                                                                                                    | 公司资料、核心指标与分红                                                                                                                                      |
| `watch`、`watch add`、`watch remove`                                                                                                                                                                                      | 自选行情总览及增删                                                                                                                                            |
| `financials`                                                                                                                                                                                                              | 三类财务报表查询及 CSV／JSON 导出                                                                                                                             |
| `annual-report`                                                                                                                                                                                                           | 按单年或年份范围下载年报 PDF                                                                                                                                  |
| `announcements`、`insider`                                                                                                                                                                                                | 公司公告、增减持公告                                                                                                                                          |
| `filings`                                                                                                                                                                                                                 | 美股 SEC 申报（10-K、10-Q、8-K 等）                                                                                                                           |
| `config`                                                                                                                                                                                                                  | CLI 与 Desktop 共用的配置，如 SEC 联系邮箱；`config ai` 管理 AI 模型配置                                                                                      |
| `ipo list / calendar / prospectus / download`                                                                                                                                                                             | 新股列表、IPO 公告日历、招股书查询与下载                                                                                                                      |
| `metal`                                                                                                                                                                                                                   | 黄金、白银日行情，`--inst` 选品种、`-b/-e` 查区间                                                                                                             |
| `commodity`                                                                                                                                                                                                               | 大宗商品期货行情，`--inst` 加 `-b/-e` 查日行情                                                                                                                |
| `fx quote`、`fx history`                                                                                                                                                                                                  | 在岸／离岸人民币、美元及港币参考行情、来源独立的新浪／FRED 日线，见 [汇率文档](docs/forex.md)                                                                 |
| `fx fixing`、`fx reference`                                                                                                                                                                                               | 人民币中间价、在岸固定时点参考汇率，保留各自价格和时间口径                                                                                                    |
| `fx cross`、`fx spread`                                                                                                                                                                                                   | 按同源同口径计算交叉汇率、在岸／离岸报价价差，保留公式、输入及时间对齐状态                                                                                    |
| `bond`、`bond-history`                                                                                                                                                                                                    | 美债收益率与历史                                                                                                                                              |
| `macro us treasury`                                                                                                                                                                                                       | 美债收益率全部期限，支持 `--standard-json`，见 [美债文档](docs/bond.md#macro-us-treasury)                                                                     |
| `macro cn cpi`、`macro cn core-cpi`、`macro cn ppi`                                                                                                                                                                       | 国家统计局月度价格指数，见 [NBS 文档](docs/nbs.md)                                                                                                            |
| `macro cn pmi`                                                                                                                                                                                                            | 国家统计局制造业 PMI 及生产、订单、出口订单、就业分项                                                                                                         |
| `macro cn nonmanufacturing-pmi` / `macro cn npmi`                                                                                                                                                                         | 非制造业、服务业、建筑业商务活动指数；`--details` 展示九个全国分项                                                                                            |
| `macro cn industrial` / `macro cn ip`                                                                                                                                                                                     | 规模以上工业增加值单月同比、累计同比及可检索的季调环比                                                                                                        |
| `macro cn retail`                                                                                                                                                                                                         | 社会消费品零售总额当月金额、同比、累计金额及累计同比                                                                                                          |
| `macro cn investment`                                                                                                                                                                                                     | 固定资产投资总量及制造业、基建、房地产分项的累计口径，见 [投资文档](docs/cn-investment.md)                                                                    |
| `macro cn property`                                                                                                                                                                                                       | 新建商品房销售面积、销售额及房屋新开工面积，见 [房地产文档](docs/cn-property.md)                                                                              |
| `macro cn enterprise`                                                                                                                                                                                                     | 规模以上工业企业利润、总存货/产成品存货及应收账款，区分累计流量与期末余额，见 [工业企业文档](docs/cn-enterprise.md)                                           |
| `macro cn household`                                                                                                                                                                                                      | 全国居民人均累计收入、消费支出及官方实际同比，见 [居民收支文档](docs/cn-household.md)                                                                         |
| `macro cn deposits`                                                                                                                                                                                                       | 住户人民币存款余额、严格基期计算同比和报告新增额，见 [居民存款文档](docs/cn-deposits.md)                                                                      |
| `macro cn fiscal`                                                                                                                                                                                                         | 全国一般公共预算、政府性基金累计收支及官方同比，见 [中国财政文档](docs/cn-fiscal.md)                                                                          |
| `macro cn trade`                                                                                                                                                                                                          | 海关货物进出口人民币/美元月度总量；支持 `--partners-currency cny` 查询人民币伙伴月值及累计值，见 [货物贸易文档](docs/cn-trade.md)                             |
| `macro cn services-trade`                                                                                                                                                                                                 | 服务贸易人民币/美元月度进出口与官方差额，保留历史缺口，见 [服务贸易文档](docs/cn-services-trade.md)                                                           |
| `macro cn unemployment`                                                                                                                                                                                                   | 全国城镇调查失业率；`--age-groups` 展示年龄分组并标记口径变化                                                                                                 |
| `macro cn financing` / `macro cn afre`                                                                                                                                                                                    | 人民银行社融存量、同比、月度/累计增量；`--details` 展示融资结构，见 [PBC 文档](docs/pbc.md)                                                                   |
| `macro cn loans`                                                                                                                                                                                                          | 金融机构人民币贷款余额、同比和新增；`--details` 展示住户／企事业单位期限结构，近似计算值带 `≈`                                                                |
| `macro cn money`                                                                                                                                                                                                          | 人民银行 M1、M2 余额及同比；标注 2025 年 M1 口径修订                                                                                                          |
| `macro cn reverse-repo`                                                                                                                                                                                                   | 七天逆回购政策利率调整公告，含公布/生效日期及前后值；覆盖自 2024-07-22 起                                                                                     |
| `macro cn reverse-repo-history`                                                                                                                                                                                           | 固定利率机制切换前七天逆回购逐笔中标利率，2015-01-22 至 2024-07-19；与政策调整事件序列分开                                                                    |
| `macro cn lpr`                                                                                                                                                                                                            | 1 年期、5 年期以上 LPR 月度报价、实际报价日、公布时间及基点变化；覆盖自 2019-08 起                                                                            |
| `macro cn rrr`                                                                                                                                                                                                            | 人民币存款准备金率调整，内置 2018 年起可逐条核验历史并增量检查新公告；区分全面/定向及分批生效                                                                 |
| `macro cn treasury`                                                                                                                                                                                                       | 中债国债到期收益率曲线，含 3 个月、6 个月及 1、2、5、10 年期；直接查询指定日期范围，见 [中债文档](docs/chinabond.md)                                          |
| `macro cn gdp`                                                                                                                                                                                                            | 国家统计局季度实际 GDP 同比、累计同比、季调环比及分段基期的不变价金额，见 [NBS 文档](docs/nbs.md#实际-gdp)                                                    |
| `macro us cpi`、`macro us core-cpi`                                                                                                                                                                                       | BLS 美国 CPI/核心 CPI 季调与非季调指数、非季调同比及季调环比；保留缺口与脚注，见 [BLS 文档](docs/bls.md)                                                      |
| `macro us pce`、`macro us core-pce`                                                                                                                                                                                       | BEA 经 FRED 转接的 PCE/核心 PCE 季调链式价格指数、同比与月环比；区别于消费支出金额和实际消费量，见 [FRED 文档](docs/fred.md)                                  |
| `macro us payrolls`                                                                                                                                                                                                       | BLS 季调非农工资岗位总数、月度变化及本机观测版本，见 [BLS 文档](docs/bls.md)                                                                                  |
| `macro us earnings`、`macro us weekly-hours`                                                                                                                                                                              | BLS 私人非农全部雇员季调平均时薪、平均每周工时、月度变化和同比；保留基期与脚注，见 [BLS 文档](docs/bls.md#平均时薪与每周工时)                                 |
| `macro us unemployment`、`macro us participation`                                                                                                                                                                         | BLS CPS 季调失业率、劳动参与率及百分点变化；保留缺口和修订脚注，见 [BLS 文档](docs/bls.md#失业率与劳动参与率)                                                 |
| `macro us initial-claims`、`macro us continued-claims`                                                                                                                                                                    | 劳工部 ETA 经 FRED 转接的季调初请/续请数量、周变化及四周计算均值；按各自周六所属周对齐，见 [FRED 文档](docs/fred.md#初请与续请失业金)                         |
| `macro us real-pce`                                                                                                                                                                                                       | BEA 经 FRED 转接的实际消费总量、商品及服务分项；保留季调折年水平与未年化增速，见 [FRED 文档](docs/fred.md#实际个人消费支出)                                   |
| `macro us real-income`                                                                                                                                                                                                    | BEA 经 FRED 转接的实际可支配个人收入总量、同比与月环比；保留 2017 年链式美元季调折年水平，见 [实际收入文档](docs/real-income.md)                              |
| `macro us industrial-production`、`macro us capacity-utilization`                                                                                                                                                         | 美联储经 FRED 转接的工业生产指数与产能利用率；`--sector total\|manufacturing` 选择总量或 NAICS 制造业，见 [工业生产文档](docs/us-production.md)               |
| `macro us effr`                                                                                                                                                                                                           | 纽约联储有效联邦基金利率、所属日期及前一可用观测值的基点变化，见 [纽约联储文档](docs/nyfed.md)                                                                |
| `macro us target-rate`                                                                                                                                                                                                    | 联邦基金目标利率区间及调整历史；区分政策公布与生效信息，见 [美联储政策利率文档](docs/federalreserve.md)                                                       |
| `macro us gdp`                                                                                                                                                                                                            | 美国实际 GDP 季度水平、同比及环比年化增速；`--vintages` 查询 BEA 官方初值与修订增速档案，见 [美国 GDP 文档](docs/us-gdp.md)                                   |
| `macro us money`、`macro us m2`                                                                                                                                                                                           | 美国 M2 月度季调余额、同比与月环比，见 [美国货币供应文档](docs/us-money.md)                                                                                   |
| `macro cn money --standard-json`、`macro us money --standard-json`                                                                                                                                                        | 货币指标统一 JSON 导出：统计口径、来源、缺失状态和原 provider 结果，见 [统一导出文档](docs/macro-standard.md)                                                 |
| `macro cn retail --standard-json`、`macro cn property --standard-json`                                                                                                                                                    | 社零、房地产统一 JSON 导出：单月／累计所属期、官方同比、历史范围及来源序列，见 [统一导出文档](docs/macro-standard.md)                                         |
| `macro us gdp --standard-json`、`macro us effr --standard-json`                                                                                                                                                           | 季度 GDP、日度 EFFR 统一 JSON 导出：折年口径、严格基期、交易日和方法边界，见 [统一导出文档](docs/macro-standard.md)                                           |
| `macro us cpi --standard-json`、`macro us core-cpi --standard-json`                                                                                                                                                       | CPI 指数与增速统一 JSON 导出：季调状态、指数基期及严格计算基期，见 [统一导出文档](docs/macro-standard.md)                                                     |
| `macro us initial-claims --standard-json`、`macro us continued-claims --standard-json`                                                                                                                                    | 周度申领统一 JSON 导出：所属周、前周变化、四周均值和输入窗口，见 [统一导出文档](docs/macro-standard.md)                                                       |
| `macro us pce --standard-json`、`macro us core-pce --standard-json`、`macro us real-pce --standard-json`                                                                                                                  | PCE 价格与实际消费统一 JSON 导出：指数／金额、折年口径和分项不可加性，见 [统一导出文档](docs/macro-standard.md)                                               |
| `macro us real-income --standard-json`、`macro us industrial-production --standard-json`、`macro us capacity-utilization --standard-json`                                                                                 | 实际收入与工业统一 JSON 导出：总体范围、折年口径和百分比／百分点变化，见 [统一导出文档](docs/macro-standard.md)                                               |
| `macro us unemployment --standard-json`、`macro us participation --standard-json`、`macro us earnings --standard-json`、`macro us weekly-hours --standard-json`                                                           | 劳动力比率、时薪工时统一 JSON 导出：CPS／CES 范围、分子分母、变化单位与初步标记，见 [统一导出文档](docs/macro-standard.md)                                    |
| `macro us fiscal --standard-json`、`macro us corporate-profits --standard-json`、`macro us business-inventories --standard-json`、`macro us new-orders --standard-json`                                                   | 财政与企业统一 JSON 导出：财年累计、赤字符号、利润流量、库存余额和订单口径，见 [统一导出文档](docs/macro-standard.md)                                         |
| `macro us bank-credit --standard-json`、`macro us lending-standards --standard-json`、`macro us debt-service --standard-json`、`macro us housing-starts --standard-json`、`macro us equipment-investment --standard-json` | 信用与投资统一 JSON 导出：月平均余额、调查净比例、百分比／百分点变化、折年水平和名义／实际口径，见 [统一导出文档](docs/macro-standard.md)                     |
| `macro us payrolls --standard-json`、`macro us housing --standard-json`、`macro us house-price --standard-json`、`macro us trade --standard-json`、`macro us real-trade --standard-json`                                  | 非农、住宅与贸易统一 JSON 导出：岗位差额、新房／成屋单位、房价指数基期、官方差额和名义／实际口径，见 [统一导出文档](docs/macro-standard.md)                   |
| `macro us target-rate --standard-json`                                                                                                                                                                                    | 联邦基金目标区间统一 JSON 导出：实际调整事件、上下限、基点变化及生效／公布日期，见 [统一导出文档](docs/macro-standard.md)                                     |
| `macro us debt --standard-json`、`macro us debt-cash --standard-json`、`macro us auctions --standard-json`                                                                                                                | 国债统一 JSON 导出：DTS 官方明细、累计值、现金总额与计算净额，逐券拍卖事件及面值分配，见 [统一导出文档](docs/macro-standard.md)                               |
| `macro cn pmi --standard-json`、`macro cn nonmanufacturing-pmi --standard-json`、`macro cn industrial --standard-json`、`macro cn unemployment --standard-json`                                                           | 中国 PMI、工业和失业率统一 JSON 导出：季调扩散指数、单月／累计／环比增速和年龄分组口径版本，见 [统一导出文档](docs/macro-standard.md)                         |
| `macro cn gdp --standard-json`、`macro cn household --standard-json`、`macro cn investment --standard-json`、`macro cn enterprise --standard-json`                                                                        | 中国 GDP、居民收支、投资和工业企业统一 JSON 导出：不变价金额与官方指数、累计范围、基建统计范围和调查范围断点，见 [统一导出文档](docs/macro-standard.md)       |
| `macro cn financing --standard-json`、`macro cn loans --standard-json`、`macro cn deposits --standard-json`                                                                                                               | 中国社融、贷款和住户存款统一 JSON 导出：存量与流量、官方与近似新增、计算累计和计算同比，见 [统一导出文档](docs/macro-standard.md)                             |
| `macro cn fiscal --standard-json`、`macro cn trade --standard-json`、`macro cn services-trade --standard-json`                                                                                                            | 中国财政和贸易统一 JSON 导出：累计范围、另列同比、双币种和主要伙伴，见 [统一导出文档](docs/macro-standard.md)                                                 |
| `macro cn cpi --standard-json`、`macro cn core-cpi --standard-json`、`macro cn ppi --standard-json`                                                                                                                       | 中国价格统一 JSON 导出：官方相对指数和计算涨跌幅分开，保留缺口与历史目录来源，见 [统一导出文档](docs/macro-standard.md)                                       |
| `macro cn lpr --standard-json`、`macro cn reverse-repo --standard-json`、`macro cn reverse-repo-history --standard-json`、`macro cn rrr --standard-json`、`macro cn treasury --standard-json`                             | 中国利率统一 JSON 导出：LPR 报价与相邻月变化、逆回购调整事件和中标历史、准备金率幅度与机构范围、全部期限国债收益率，见 [统一导出文档](docs/macro-standard.md) |
| `macro us trade`、`macro us real-trade`                                                                                                                                                                                   | 月度名义贸易与季度实际贸易，出口、进口及官方差额；支持商品/服务分项，见 [美国进出口文档](docs/us-trade.md)                                                    |
| `macro us housing-starts`、`macro us equipment-investment`                                                                                                                                                                | 住宅开工、建筑许可及季度设备投资，分别保留频率、单位和季调折年口径，见 [美国投资文档](docs/us-investment.md)                                                  |
| `macro us bank-credit`、`macro us lending-standards`、`macro us debt-service`                                                                                                                                             | 月平均银行信贷、季度贷款标准及家庭偿债负担，区分百分比和百分点变化，见 [美国信用文档](docs/us-credit.md)                                                      |
| `macro us fiscal`                                                                                                                                                                                                         | 联邦月度收入、支出、官方盈余/赤字及计算财年累计，见 [美国财政文档](docs/us-fiscal.md)                                                                         |
| `macro us debt`                                                                                                                                                                                                           | 财政部每日可流通／非流通国债记账发行、赎回及计算净额，保留调整与累计值，见 [美国国债交易文档](docs/us-debt.md)                                                |
| `macro us debt-cash`                                                                                                                                                                                                      | 国债官方现金发行、现金赎回、计算净现金流及 IIIB 调整溯源，见 [债务现金流文档](docs/us-debt-cash.md)                                                           |
| `macro us auctions`                                                                                                                                                                                                       | 国债逐券发行日期、拍卖面值与分配、SOMA 及再发行事件，见 [美国拍卖文档](docs/us-auctions.md)                                                                   |
| `macro us corporate-profits`、`macro us business-inventories`、`macro us new-orders`                                                                                                                                      | 企业税前利润、商业库存及制造业新订单，分开保留季度流量、月末余额和月度流量，见 [美国企业文档](docs/us-enterprise.md)                                          |
| `macro us housing`、`macro us house-price`                                                                                                                                                                                | 新房与成屋销售、FHFA 季度房价指数，保留各自历史覆盖和统计口径，见 [美国房地产文档](docs/us-housing.md)                                                        |
| `strategy ma / macd / rsi / boll`                                                                                                                                                                                         | 技术指标与信号，`--backtest` 回测                                                                                                                             |
| `valuation`                                                                                                                                                                                                               | 估值指标与 DCF 分析                                                                                                                                           |
| `upgrade`、`--version`                                                                                                                                                                                                    | CLI 升级与版本信息                                                                                                                                            |

只读数据命令都支持 `--json`，K 线和技术分析支持 `--freq day|week|month`，约定见 [命令对照文档](docs/commands.md#脚本调用)。

使用 `sec <命令> --help` 查看参数，IPO 等子命令使用 `sec ipo <子命令> --help`。

```shell
# 自选与 Desktop 共享
sec watch add SH600036 SZ002475
sec watch

# 财报查询与导出
sec financials SH600036 --type balance --period annual --output balance.csv
sec annual-report SH600036 --start-year 2023 --end-year 2025 --output-dir ./reports

# 新股上市日期范围、招股书查询
sec ipo list --since 2026-01-01 --until 2026-06-30
sec ipo prospectus 300750

# 技术分析与估值
sec strategy macd SH600036
sec strategy ma SH600036 -b 20210101 -e 20251231 --backtest
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

### 查看贵金属价格走势

```shell
$ sec metal
  日期       | 品种             | 收盘              | 开盘 | 最高   | 最低 | 单位
  2026-11-03 | Au99.99 黄金现货 | 840.01 3.93 0.47% | 836  | 849.99 | 834  | 元/克

$ sec metal -i ag999 -b 20261028
  日期       | 品种             | 收盘              | 开盘  | 最高  | 最低  | 单位
  2026-10-28 | Ag99.99 白银现货 | 13075 255 1.99%   | 13075 | 13075 | 13075 | 元/千克
  2026-10-29 | Ag99.99 白银现货 | 12730 -345 -2.64% | 12730 | 12730 | 12730 | 元/千克
  2026-10-30 | Ag99.99 白银现货 | 12244 -486 -3.82% | 12335 | 12335 | 12150 | 元/千克
  2026-11-02 | Ag99.99 白银现货 | 11876 -368 -3.01% | 11866 | 11955 | 11866 | 元/千克
  2026-11-03 | Ag99.99 白银现货 | 11747 -129 -1.09% | 11747 | 11747 | 11747 | 元/千克
```

### 查看大宗商品行情

```shell
$ sec commodity
  代码  | 品种       | 最新    | 涨跌     | 涨跌幅 | 开盘   | 最高   | 最低   | 昨结算 | 单位    | 时间
  sc    | 原油       | 698.2   | -47.00   | -6.31% | 717.6  | 719.7  | 677.3  | 745.2  | 元/桶   | 2026-11-03 15:00:00
  cu    | 铜         | 106090  | -1490.00 | -1.39% | 107810 | 108470 | 106090 | 107580 | 元/吨   | 2026-11-03 15:00:00
  i     | 铁矿石     | 671     | -5.00    | -0.74% | 673    | 675.5  | 668    | 676    | 元/吨   | 2026-11-03 15:04:56
  rb    | 螺纹钢     | 2991    | -28.00   | -0.93% | 3005   | 3019   | 2990   | 3019   | 元/吨   | 2026-11-03 15:00:00
  m     | 豆粕       | 3147    | -40.00   | -1.26% | 3198   | 3204   | 3138   | 3187   | 元/吨   | 2026-11-03 15:04:56
  cl    | WTI 原油   | 98.485  | +2.03    | +2.10% | 96.3   | 98.69  | 96.12  | 96.46  | 美元/桶 | 2026-11-03 19:34:10
  brent | 布伦特原油 | 106.494 | +1.72    | +1.65% | 104.45 | 107.24 | 104.27 | 104.77 | 美元/桶 | 2026-11-03 19:34:14
  hg    | COMEX 铜   | 635.582 | -21.17   | -3.22% | 653.75 | 655.25 | 631.8  | 656.75 | 美分/磅 | 2026-11-03 19:34:03

国内为主力连续合约，换月时价格会跳空；涨跌相对昨日结算价。行情来自新浪期货，有延迟，不构成投资建议。

$ sec commodity -i cu -b 20261030
铜（cu，上海期货交易所），单位 元/吨

  日期       | 收盘   | 涨跌     | 涨跌幅 | 开盘   | 最高   | 最低   | 结算   | 成交量
  2026-10-30 | 108050 | -980.00  | -0.90% | 108160 | 108650 | 107870 | 108190 | 89853
  2026-11-02 | 107810 | -380.00  | -0.35% | 107720 | 108010 | 107160 | 107580 | 104635
  2026-11-03 | 106090 | -1490.00 | -1.39% | 107810 | 108470 | 106090 | 107230 | 126807

涨跌相对前一日结算价；主力连续合约换月时价格会跳空。
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

CLI 使用 Go 1.26.11 或更新版本：

```shell
go build -o sec .
go test -v ./...
```

macOS Desktop 还需要 Node.js（Release 使用 Node 24）、npm 与 Xcode Command Line Tools，先构建前端，再编译桌面端：

```shell
npm ci --prefix desktop/ui
npm run build --prefix desktop/ui
go build -o sec-desktop ./desktop
./sec-desktop
```

开发联调、应用打包与架构说明见 [Desktop 文档](docs/desktop.md)。

## 开发计划

### TODO：Desktop 功能补齐

已覆盖证券搜索、公司核心指标与分红、自选行情总览与多证券对比、基础行情与蜡烛图（含日期范围、快捷区间、复权选择及 MA、布林带叠加）、三类财报及 CSV/JSON 导出、定期报告（年报／半年报／一季报／三季报）年度范围查询与单份／多选 PDF 下载（含任务进度）、公告查询（含年报／中报／一季报／三季报类别筛选）、增减持公告、技术分析（双均线、MACD、RSI、布林带及信号明细）、估值（PE、PB、PS 历史分位，PEG、ROE、格雷厄姆数及 DCF）、IPO 新股上市日期范围筛选与发行公告日期视图、招股说明书及相关 PDF 查询与单份／多选下载、中美宏观指标（`sec macro` 全部指标）、汇率（`sec fx` 全部命令）、贵金属行情（Au99.99、Ag99.99）、大宗商品行情与日 K 线、美债收益率及设置页版本详情与更新入口。目前 CLI 的数据命令都已有对应的 Desktop 页面。

## 致谢

- [efinance](https://efinance.readthedocs.io/)
- [akshare](https://github.com/akfamily/akshare)
- [rains](https://github.com/rookie0/rains)
