## sec change log

### v0.4.0

1. Desktop 新增「黄金行情」，复用 CLI metal / metal-history 的上海黄金交易所 Au99.99 数据，展示最新日行情、涨跌及元/克单位
2. 支持自定义日期范围、近 1 个月 / 3 个月 / 1 年快捷查询、历史日 K 线蜡烛图与分页明细，提供手动刷新、重试和空数据提示
3. 新增黄金行情 API 与日期校验，保留区间外昨收基准，未知涨跌显示缺失值；黄金 K 线支持开高低收查看、键盘选择和长区间横向滚动
4. 修复黄金 K 线长区间滚动后价格刻度不可见的问题，将价格轴固定在滚动区域外，并同步调整鼠标与键盘选择坐标
5. 「自选与行情」中的证券历史改用日 K 线蜡烛图，历史接口补充开高低收、成交量与成交额，增加成交量副图和逐日量额信息，复用黄金的固定价格轴与交互，保留沪深/港股与不复权口径
6. Add Desktop Treasury yields with the latest 11-tenor curve, selectable historical trends, date filters, daily basis-point changes, and paginated records
7. Preserve missing Treasury yields as null values, retain cross-year comparison baselines, and reject incomplete annual responses instead of displaying partial history
8. Add industry, PE (TTM), PB, total market capitalization, and float market capitalization to Desktop overview and company pages using shared CLI info calculations; load metrics independently, show unavailable values explicitly, and normalize quoted profile fields
9. Add Desktop financial statement CSV/JSON exports with original fields and units, plus annual report lookup by fiscal year and PDF downloads; save files to a configurable download directory (default ~/Downloads) without overwriting existing downloads
10. Move Desktop security search into the Stocks and Watchlist page above the security list, keeping search results and the selected security when switching pages
11. Add Desktop security history date filters, 1/3/6-month and 1-year shortcuts, and unadjusted/forward/backward adjustment choices; retain the full requested range with a five-year limit and default to the last three months
12. Add Desktop IPO prospectus lookup by security code and disclosure dates, paginated original PDF links, and single or selected downloads with progress, cancellation, per-file failures, and the configured download directory; consolidate the IPO module into New Listings and Prospectuses, removing the duplicate listing timeline
13. Add annual, half-year, first-quarter, and third-quarter announcement filters to the Desktop announcement center and security research, combining category selection with title search and resetting pagination when categories change; strip upstream search highlight tags from security names
14. Add Desktop annual report year-range queries and current-page multi-select downloads with sequential task progress, success/failure counts, per-file errors, and cancellation that preserves completed files
15. Expand Desktop Annual Reports into Periodic Reports with annual, half-year, first-quarter, and third-quarter PDF queries and batch downloads; support current-year interim reports and distinguish report types in downloaded filenames
16. Replace Desktop sidebar text symbols with statically imported Lucide SVG icons, using consistent sizing and strokes that follow theme and active navigation colors
17. Replace the Desktop header lettermark with the existing sec PNG logo, keeping it visible and proportionally sized on narrow screens; use uppercase SEC branding in the header, window title, About section, and macOS display name
18. Unify CLI and Desktop watchlists through a shared store with cross-process locks, atomic mutations, legacy-list merging and backups; separate Desktop settings writes from watchlist changes, refresh shared selections on return/focus, document shared strategy/valuation extraction TODOs, and isolate the JSON utility test temporary directory to prevent deleting other build artifacts
19. Add inclusive, optional listing-date bounds and reset controls to Desktop New Listings; share date validation and filtering with CLI IPO lists, filter before pagination, and exclude unknown dates only when filtering
20. Add Desktop version, build time, commit, and platform details with manual update checks and official Release/download links; share GitHub release queries with CLI and match Desktop DMGs by version and architecture
21. Refresh Desktop layout with a full-height branded sidebar, compact page headers and shared page actions, neutral light/dark themes, denser research summaries, grouped settings, sticky table headers, and responsive stacked layouts inspired by CC Switch
22. Simplify the Desktop sidebar brand to the logo and SEC label, removing the subtitle and unused styles
23. Update README with the current CLI and Desktop feature matrix, installation and build instructions, shared watchlist usage, and five Desktop screenshots using hosted images
24. Add official NBS monthly CPI, core CPI, and PPI collection with dynamic catalog discovery, historical partition handling, original comparison indexes, derived year-over-year/month-over-month changes, and explicit missing values
25. Add `macro cn` price queries with month filters, JSON output, six-hour raw-response caching, refresh controls, NBS source and methodology documentation, and separate CN/US macro collection TODOs
26. Add `macro cn pmi` to collect official manufacturing PMI, production, new orders, new export orders, and employment indexes with monthly tables, JSON export, shared cache controls, explicit missing values, and seasonally adjusted diffusion-index metadata
27. Add `macro cn nonmanufacturing-pmi` (`npmi`) with national, services, and construction business activity indexes plus nine detailed components; share PMI collection and cache controls, distinguish survey types in JSON, and preserve documented historical gaps
28. Add `macro cn industrial` (`ip`) for real monthly and year-to-date industrial value-added growth, supplement seasonally adjusted monthly growth from official release revision tables, preserve January–February reporting gaps, and document source timestamps, caching, and historical coverage limits
29. Add `macro cn retail` for national monthly and year-to-date retail sales amounts and nominal growth, preserving CNY 100 million units, January–February reporting gaps, source identifiers, JSON exports, and shared cache controls without mixing in above-designated-size retail series
30. Add `macro cn unemployment` with optional age groups, explicit student-coverage definitions, separate comparable series keys across the December 2023 methodology change, preserved publication gaps, JSON export, and shared cache controls
31. Add `macro cn financing` (`afre`) using official PBC annual stock and flow tables, with financing components, normalized CNY 100 million amounts, explicitly derived year-to-date flows, source notes, JSON exports, and six-hour caching
32. Add `macro cn loans` for PBC financial-institution RMB loan balances, official year-over-year growth, monthly and year-to-date additions, and household/enterprise maturity breakdowns; mark differences of rounded cumulative releases as approximate and preserve report gaps
33. Add `macro cn money` for official monthly M1 and M2 balances and year-over-year growth, label the 2025 M1 definition change, and retain missing report months as null observations
34. Add `macro cn reverse-repo` for official seven-day reverse repo policy-rate changes since July 22, 2024, with separate publication and effective dates, before/after rates, calculated basis-point changes, preceding-event context, and JSON exports
35. Add `macro cn lpr` for official monthly one-year and five-year-plus LPR quotes since August 2019, with actual quote dates, publication metadata, independent adjacent-month basis-point changes, explicit missing values, and JSON exports
36. Add `macro cn rrr` for RMB reserve requirement adjustments announced since 2020, preserving institution scopes, exemptions, targeted ranges, additional cuts, phased effective dates, and official sources
37. Embed verified RRR history and persist incremental announcements with same-day checks, deduplication, and atomic checkpoints; serve covered historical queries offline and check only recent news pages for updates
38. Add `macro cn treasury` for official ChinaBond government bond yield-to-maturity curves at one, two, five, and ten years since March 2006, with annual batch queries, six-hour raw-response caching, Beijing dates, explicit missing tenors, source metadata, and JSON exports
39. Use pointer requests and results for ChinaBond Treasury collection, reject nil requests, and return nil results on errors
40. Restore the ChinaBond historyQuery GET collector and request/response structures removed in PR #66; call GetChinaBond directly from macro Treasury queries, output original fields in JSON, display three-month and six-month yields alongside the existing annual tenors, preserve source precision, and remove the unused Treasury wrapper and annual cache controls
41. Add `macro cn gdp` for official NBS quarterly real GDP levels, quarterly and year-to-date growth indexes, derived year-over-year growth, and seasonally adjusted non-annualized quarter-over-quarter growth; preserve missing values, source annotations, segmented price-base limitations, latest-available revision metadata, and shared cache controls
42. Add `macro us cpi` and `macro us core-cpi` using official BLS CPI-U series, with seasonally adjusted and unadjusted indexes, explicitly derived growth and comparison baselines, preserved gaps and footnotes, ten-year batch queries, shared six-hour caching, and JSON exports
43. Add `macro us pce` and `macro us core-pce` for BEA chain-type price indexes distributed by FRED, with seasonally adjusted year-over-year and monthly growth, explicit comparison baselines and missing values, shared full-history CSV caching, and source/version documentation distinct from consumption amounts and volumes
44. Add `macro us payrolls` for BLS seasonally adjusted nonfarm payroll job levels and adjacent-month changes, preserving preliminary footnotes, missing values, shared six-hour caching, and locally observed version history; document that official first/second/third estimates and historical vintage archives remain pending
45. Keep payroll monthly changes within a single response across batch boundaries, persist observation timestamps even when values are unchanged to reject late snapshots, and validate existing archives before writing; preserve legacy history through migration and cover revision, concurrency, missing-baseline, and corrupt-archive cases with regression tests
46. Add `macro us earnings` and `macro us weekly-hours` for BLS seasonally adjusted average hourly earnings and weekly hours of all private nonfarm employees, with explicit units and coverage, monthly level changes, same-response growth baselines, preliminary footnotes, missing values, shared caching, and JSON exports
47. Add `macro us unemployment` and `macro us participation` for official BLS CPS seasonally adjusted rates, with percentage-point changes, explicit population and denominator definitions, same-response comparison baselines, missing observations and population-control revision footnotes, validated rate bounds, shared caching, and JSON exports
48. Add `macro us initial-claims` and `macro us continued-claims` for seasonally adjusted ETA data distributed by FRED, preserving each series' Saturday observation dates and release lag, strict weekly changes and calculated four-week averages, missing values and comparison windows; share validated CSV caching with PCE while keeping metric caches separate
49. Add `macro us real-pce` for BEA real consumption totals and official goods, durable goods, nondurable goods, and services components distributed by FRED; preserve chained 2017 dollar SAAR levels, non-annualized growth, strict comparison baselines, missing values, component non-additivity, and shared snapshot caching with JSON exports
50. Add `macro us real-income` for BEA real disposable personal income distributed by FRED, preserving chained 2017 dollar SAAR levels, non-annualized growth, strict comparison baselines, missing values, source metadata, and validated snapshot caching
51. Add `macro us industrial-production` and `macro us capacity-utilization` for Federal Reserve total industry and NAICS manufacturing data distributed by FRED, with separate history coverage, index growth and utilization percentage-point changes, shared caching, and JSON exports
52. Add `macro us effr` using the New York Fed Markets API, preserving effective dates, prior-observation basis-point changes, revision flags and raw fields; handle the 2016 methodology boundary, omit unpublished dates, and document unavailable publication timestamps and historical vintages
53. Add `macro us target-rate` to collect Federal Reserve target-range adjustments, with prior-range baselines, basis-point changes, independently verified announcement and effective dates, raw source fields, and validated page caching
54. Add `macro us gdp` for real GDP levels and official annualized quarterly growth distributed by FRED, with strict non-annualized comparison rates and optional BEA release-vintage growth records from 2002Q1; keep unavailable historical real levels and publication times null
55. Add `macro us money` / `m2` for Federal Reserve monthly seasonally adjusted M2 balances distributed by FRED, with strict year-over-year and monthly growth, raw values, methodology notes, validated snapshot caching, and updated macro collection documentation and TODO status
56. Add `macro cn investment` for cumulative total, manufacturing, infrastructure and real estate development investment from official NBS monthly releases, preserving available amounts, comparable-basis notes, publication times and infrastructure scope changes
57. Add `macro cn property` for cumulative new commercial building sales area, sales value and housing starts area with official growth rates, historical sales scope metadata, missing values and JSON exports
58. Add `macro us trade` and `macro us real-trade` for monthly nominal and quarterly real exports, imports and official aggregate balances, with goods and services components, distinct units and accounting bases, strict comparison periods and validated FRED snapshot caching
59. Validate NBS investment release directories and property metadata and observations before caching, preserve valid cached responses on failed refreshes, recover invalid cached responses, and document collection coverage and remaining real trade component balances
60. Add Chinese industrial enterprise profit, total and finished-goods inventories, and accounts receivable collection, preserving cumulative flows, period-end balances, official comparable growth rates and statistical scope metadata
61. Add US housing starts, building permits and quarterly equipment investment, preserving nominal and real measures, distinct frequencies, annualized levels and strict calendar comparison baselines
62. Add US new and existing home sales and the FHFA quarterly house price index, retaining source units, seasonal adjustment, snapshot coverage limits and validated response caching
63. Simplify the Chinese industrial enterprise terminal table to five columns, keeping survey scope and detailed notes in JSON output and shortening the footer
64. Add US corporate profits with IVA and CCAdj, total business inventories and manufacturing new orders with distinct frequencies, units and flow or stock definitions, strict comparison bases, validated FRED caching and compact terminal tables
65. Add US monthly bank credit, quarterly lending standards and household debt service ratios with distinct percentage and percentage-point changes, survey timing, current methodology coverage, validated FRED snapshots and compact terminal output
66. Add Chinese household per-capita cumulative income and consumption with official real growth, integrated survey coverage, annual own-production scope, raw values, validated NBS response caching and compact terminal output

67. Add Chinese household RMB deposit balances, demand and time components, strict prior-year calculated growth and reported deposit flows, with explicit approximate monthly differences, source metadata and validated PBC caching
68. Add Chinese national general public budget and government-managed fund cumulative receipts and expenditures, preserving official, comparable and tax-refund-adjusted growth, combined January-February periods, historical directory gaps and validated MOF page caching
69. Add US monthly federal receipts, outlays and official surplus or deficit through FRED, with explicit sign conventions, strict calculated fiscal-year totals, missing observations and validated snapshots; keep Treasury issuance and official comparable household deposit growth as separate TODOs
70. Add `macro cn trade` for separate CNY and USD goods-trade data, `macro cn reverse-repo-history` for verified seven-day reverse repo rates, and extend the embedded RRR archive to 2018; handle MOFCOM TLS compatibility and duplicate directory records with regression tests
71. Restore both phased city-bank RRR cuts from the September 2019 announcement, retain monthly trade data when partner observations are unavailable, and isolate mocked HTTP transports; add complete-event, missing-partner and cross-page regression coverage
72. Add `fx quote`, `fx history`, `fx fixing` and `fx reference` for CNY, CNH, USD and HKD using Sina, FRED and CFETS; preserve source-specific price and time definitions, raw values, missing and anomalous observations, validated caching and historical coverage with offline regression tests
73. Map CFETS central parity values using returned currency columns regardless of request order, retain strict currency-set validation with regression tests, and use English descriptions and flag help for all FX commands
74. Add derived FX cross rates and CNY/CNH quote spreads with formulas, original inputs, strict timestamp and quote-age checks, and explicit approximate alignment; format terminal spread timestamps in Beijing time and retain regression coverage for currency direction, quote validity and derived outputs
75. Add monthly Chinese services-trade collection in CNY and USD and optional CNY goods-trade partner reports; preserve official balances, monthly and cumulative definitions, missing periods, overlapping partner scopes and raw source values with pagination, cache and command regression tests
76. Add `macro us debt` for Treasury DTS daily recorded issues and redemptions by marketability, with explicitly derived daily summaries and net recorded issuance; preserve signed adjustments, official cumulative values, raw rows and missing values with validated pagination, caching and regression tests
77. Add `macro us auctions` for CUSIP-level issuance and reopening events with separate issue-date and auction-date filters; preserve official par amounts, bidder allocations, SOMA/FIMA inclusion definitions and historical nulls with validated pagination, caching and regression tests, including auction-date pagination and future issue-date boundaries
78. Add `macro us debt-cash` for official Treasury cash issues and redemptions, explicitly derived net cash flows and original cash-basis adjustment rows; preserve historical labels, signed adjustments, missing values and official cumulative amounts with validated pagination, caching and regression tests
79. Add opt-in `--standard-json` exports for Chinese M1/M2 and US M2 with a versioned macro series schema, explicit statistical definitions, official versus derived values, missing-value reasons and source provenance; separate Chinese M1 definitions across 2025 and preserve original provider results and strict growth baselines with adapter and CLI regression tests
80. Extend `--standard-json` to Chinese retail sales and property data with explicit monthly versus year-to-date coverage and upstream series identifiers; retain January-February combined reporting, official growth rates, area versus value units and historical sales scope boundaries without deriving monthly values, with adapter and CLI regression tests
81. Extend `--standard-json` to US quarterly GDP and daily EFFR with frequency-aware period validation, distinct GDP annualization and reference-year metadata, strict growth baselines and EFFR methodology boundaries; preserve revision flags, unknown out-of-window baseline levels and original GDP vintage records with adapter and CLI regression tests
82. Extend `--standard-json` to US CPI/core CPI and initial/continued claims with distinct seasonal-adjustment series, explicit index bases, weekly period support and complete four-week calculation inputs; preserve strict growth baselines, separate claims dates, raw values, missing observations and original footnotes with adapter and CLI regression tests
83. Extend `--standard-json` to PCE/core PCE price indexes and all five real consumption components, distinguishing price indexes from real SAAR amounts and unannualized growth; preserve strict monthly baselines, raw values, original provider results and explicit chain-component nonadditivity with adapter and CLI regression tests
84. Extend `--standard-json` to real disposable income, industrial production and capacity utilization, preserving real SAAR income totals, separate industrial sector coverage and percentage versus percentage-point changes; retain strict baselines, raw values, missing observations and distinct zero-baseline rules with adapter and CLI regression tests
85. Extend `--standard-json` to US unemployment, labor force participation, hourly earnings and weekly hours with explicit CPS/CES survey coverage, ratio numerators and denominators, native-unit versus percentage-point changes and optional preliminary flags; preserve strict same-series baselines, per-point fetch times and original footnotes with adapter and CLI regression tests
86. Extend `--standard-json` to US fiscal flows, corporate profits, business inventories and manufacturing orders; distinguish official balances from derived deficit signs and fiscal-year totals, preserve quarterly SAAR versus monthly stock/flow definitions, strict baselines and nonpositive-base handling with adapter and CLI regression tests
87. Extend `--standard-json` to US bank credit, lending standards, household debt service, housing starts, building permits and equipment investment; distinguish monthly average balances, survey net shares, current-methodology ratios and SAAR levels, separate percent from percentage-point changes and nominal from real series, and preserve strict baselines, raw values, zero-baseline handling and original provider results with adapter and CLI regression tests
88. Stop attaching per-row provider diagnostics to available `--standard-json` observations for US money, CPI, claims, labor, PCE, real consumption and income, production, enterprise, credit and investment exports; merge PCE, real consumption and income, production, enterprise, credit and investment onto one periodic adapter that rejects derived values without a usable current level or baseline, and share the CLI flag and output helpers
89. Extend `--standard-json` to US payrolls, new and existing home sales, the FHFA house price index, and nominal and real trade; keep job counts as level differences, housing units and index bases separate, and official trade balances with native-unit changes, omit uncollected real component net exports, and support multiple series in one periodic dataset with adapter and CLI regression tests
90. Extend `--standard-json` to the US federal funds target range with a new event frequency that lists only actual adjustments in the window; separate lower and upper bounds from basis-point changes and effective dates from verified release dates, avoid cross-regime changes at the 2008 range start, keep the baseline and current range in the original result, and fail the export when statement or calendar pages cannot be downloaded, with adapter and CLI regression tests
91. Extend `--standard-json` to US Treasury debt transactions, cash issues and redemptions, and auctions; keep official daily, month-to-date and fiscal-year-to-date amounts apart from derived totals and net values, keep cash adjustments as provenance only, and add an optional `event_id` and top-level `events` so auctions on the same date stay distinct; exclude identical upstream duplicate rows from debt totals, with adapter and CLI regression tests

### v0.3.15

1. Desktop 升级为研究工作台：顶部全局搜索、左侧工作空间导航、中间证券列表、右侧个股研究，切换功能时保留当前证券
2. 个股研究新增公司资料、财报、分红与公告栏目，支持三类财报及报告期切换，概览展示近 60 个交易日收盘价走势
3. 新增 IPO 上市排期与列表、公告中心及分页查询，公告原文通过系统浏览器打开并支持复制链接
4. 新增自选股增删与本地持久化，设置页支持刷新间隔与深色、浅色、跟随系统外观，数据保存至 `~/.sec/desktop/workspace.json`
5. 桌面接口补充鉴权、参数校验、写入同源检查与原子文件替换，增加接口映射与持久化测试；切换页面取消旧请求并停止行情轮询
6. 修复新浪未声明 GBK 编码的响应被清空，兼容 UTF-8 页面
7. README 新增 Desktop WebView 编译警告待办，记录新版 Clang 警告原因、临时隐藏命令与依赖修复方向
8. 将本地页面布局参考与工作产物目录 `/work/` 加入 Git 忽略规则
9. macOS Desktop Release 安装包文件名复用构建版本 `SEC_VERSION`，例如 `sec-v0.3.14-desktop-darwin-arm64.dmg`，打包、校验与上传统一使用带版本的文件名

### v0.3.14

1. 新增 sec Desktop 应用 logo 与 macOS 多尺寸 `.icns` 图标，配置应用 bundle 图标资源
2. 新增 `desktop/package-macos.sh`，统一本地与 Release 的 `.app` 打包流程，将图标集成到 macOS 安装包
3. 更新桌面版构建文档，补充本地打包、启动和应用图标说明
4. 修复 macOS 桌面版组装 `.app` 后未重新签署应用包的问题，打包完成后执行 ad-hoc 签名与严格完整性校验
5. Release 新增 DMG 完整性校验，并挂载最终镜像验证内部应用签名，校验失败时阻止上传
6. 修正 macOS 安装说明，区分签名完整性与 Developer ID 签名、公证及 Gatekeeper 信任

### v0.3.13

1. 新增桌面版（Desktop）MVP：参考 Ollama Desktop 架构，Go + 系统 WebView（webview_go）+ React 前端，本地 HTTP JSON API 复用 `provider/` 数据层
2. 支持证券搜索（`/api/v1/search`）与实时行情（`/api/v1/quote`），前端每 5 秒轮询刷新
3. 新增 `docs/desktop.md` 设计文档：记录架构、目录结构、API、构建与分阶段计划
4. `provider/sina` 的 `BasicSecurity`、`SecurityQuote` 补充 JSON tag，供桌面 API 序列化
5. 修复 `QuerySecQuote` 美股行情 key 归一化（`$`→`gb_`），并给 A/H/美股行情解析加长度守卫，避免选中美股搜索结果（如「美团」ADR）时越界 panic
6. GitHub Release workflow 新增 macOS 桌面端安装包：构建前端 + 桌面二进制，封装为 `.app` 并打包成 `.dmg` 上传到 Release（新增 `desktop/Info.plist`）
7. 修正 Release workflow 中 CLI 构建的 Go 版本 1.22 → 1.25，与 `go.mod` 对齐

### v0.3.12

1. 新增 AGENT.md

### v0.3.11

1. 新增 `sec ipo download` 命令：支持交互选择、批量下载、指定编号下载招股说明书 PDF
2. 数据来源：巨潮资讯 cninfo（复用 `cninfo.QueryIPOs` + `cninfo.DownloadPDF`）
3. 提取 `resolveStock` 公共函数，供 `prospectus` 和 `download` 命令共用
4. 支持 `--all`（全部下载）、`--index`（指定编号）、`--force`（覆盖已有）、`--dry-run`（预览）、`-o`（输出目录）等参数

### v0.3.10

1. 修复依赖报错 https://github.com/alwqx/sec/security/dependabot/3

### v0.3.9

1. 新增 `sec ipo` 命令族：`ipo list`（新股上市列表）、`ipo calendar`（新股排期视图）、`ipo prospectus`（IPO 招股说明书 PDF 链接）
2. 数据来源：东方财富 push2（新股列表）+ 巨潮资讯 cninfo（IPO 公告 / 招股书）
3. `cninfo.QueryIPOs`、`cninfo.QueryIPOByDateRange` 新增：对 IPO 公告自动填充日期和 PDF 直链
4. 东方财富 push2 对 Go 的 keep-alive 连接返回 EOF：新增 `newHTTPClient` 禁用 keep-alive 和 IPv6，`fetchIPOListBatch` 内置 5 次指数退避重试

### v0.3.8

1. 初始化 `sec insider` 命令
2. 修正输出函数，传出 io.Writer

### v0.3.7

1. 初始化 `sec watch` 命令
2. `sec kline` 新增叠加指标
3. 初始化 `sec announcements` 命令

### v0.3.6

1. `sec upgrade` 升级时下载文件大小使用 HumanByte 可读改造
2. `HumanByte/HumanNum` 从 types 迁移到 utils

### v0.3.5

1. `sec strategy` 基础策略
2. 格式化 markdown

### v0.3.4

1. 新增 `sec valuation` 命令，给个股粗略估值

### v0.3.3

1. 修复 cmd 中 RunE 和 Run 重复
2. 修复 sina.go 中赋值错误
3. 修复 cmd/quote/quote.go 中上下文丢失
4. 删除 types.go 中重复 JSONify
5. 修复 typos
6. adjustRespBodyByEncode 读完 body 后关闭 http 连接
7. 修复参数判断
8. 统一在 ParseBeginEnd 中解析 begin/end 时间
9. 修复 bug，删除冗余代码
10. 修改 log，删除无用赋值

### v0.3.2

1. `sec balance-sheet` 支持下载报表信息
2. 新增 HumanBytes 优化文件大小显示

### v0.3.1

1. `sec balance-sheet` 显示财报信息

### v0.3.0

1. 修复 `sec upgrade` 报错

   > 2026/05/14 20:29:24 ERROR replaceBinary failed to remove tmpPath tmpPath=/ error="remove : no such file or directory"

2. `sec kline` 添加基本证券信息
3. 更新 README

### v0.2.16

1. `sec kline` 终端绘制 K 线图显示行情数据

### v0.2.15

1. 优化 `quote-history` 命令内部时间范围逻辑
2. 添加 [opencode.json](https://opencode.ai/docs/zh-cn/config/) 配置
3. 美债收益率
4. 新增文档 `docs/basic.md` 和 `docs/quote.md`
5. utils.MakeRequest 新增 timeout 参数
6. 新增 `sec upgrade` 自更新命令

### v0.2.14

1. `sec metal` 获取贵金属价格，默认 Au999

### v0.2.13

1. 移除 bond 相关代码

### v0.2.12

[Deprecation of Node 20 on GitHub Actions runners](https://github.blog/changelog/2025-09-19-deprecation-of-node-20-on-github-actions-runners/)

1. actions/checkout@v4 -> v6
2. actions/setup-go@v4 -> v6
3. `bond` 命令只返回最新一条数据
4. 新增 `bond—history` 命令

### v0.2.11

1. github tag `v0.2.10` 误设置成不可变导致发版失败，改正后新建 `v0.2.11`

### v0.2.10

1. `sec bond` 打印中国国债信息
2. 使用 `cmd.Context` 作为接口调用中的 ctx

### v0.2.9

1. 修复 [issue-57](https://github.com/alwqx/sec/issues/57)：`sec q nvdia` 查不到结果 hang 住
2. sina 返回指针、QuoteWs 接口添加 context.Context 参数
3. provider 中接口添加 context.Context 参数
4. 修正 slog 用法

### v0.2.8

1. 更新`quote-history`命令描述
2. `quote-history` 支持港股
3. `quote-history` 支持美股
4. `quote-history` A 股成交量单位从 `手` 改为 `股`，要乘 100
5. `quote` 支持美股
6. 东方财富 行情接口支持香港证券交易所

### v0.2.7

1. `quote-history` 命令添加选项 `begin`、`end`
2. `quote-history` 命令添加选项 `fqt` 复权类型，选项为
   - bfq 不复权
   - qfq 前复权
   - hfq 后复权

### v0.2.6

1. 修复 github workflow 权限
2. 修复 go.mod 中 `golang.org/x/net` dependabot 版本告警

### v0.2.5

`quote-history` 命令改进：

1. 输出中，将`涨跌幅`、`涨跌额` 合并到收盘价
2. 证券代码添加交易所前缀
3. 多个行情信息，按照时间`升序`、`降序`排列

### v0.2.4

1. 拆出 `utils`
2. `quote-history` 命令初始化

### v0.2.3

1. 删除 debug 日志
2. 添加短命令 `info->i` `quote-q` `search-s`
3. 修复 info 等查询美股 panic 问题

### v0.2.2

1. 修复 A 股只判断上交所，没有判断深交所和北京交所的问题

### v0.2.1

1. 更新版本信息

### v0.2.0

1. sina 搜索接口返回数据新增一个字段，支持该变化
2. `info`/`quote` 支持港股

### v0.1.4

1. 股票信息打印价格变化

### v0.1.3

1. 全部命令添加 `--debug`/`-D` flag
2. quote 命令最多支持查询 5 个证券
3. quote 支持实时 (3s) 更新行情信息

### v0.1.2

1. 修复行情命令输出格式不稳定
2. 对多个证券进行排序

### v0.1.1

1. 添加 github actions

### v0.1.0

1. quote 拆分成子命令
