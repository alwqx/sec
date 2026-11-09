## sec change log

### v0.4.9

1. Add the `ai` section of `~/.sec/config.json` for model profiles shared by the CLI and Desktop.
2. Save other settings while the `ai` section is invalid, keeping it as is, so Desktop saves no longer fail.
3. Encrypt AI API keys with AES-256-GCM and a separate `~/.sec/secret.key`, and resolve keys from env vars or the stored key.
4. Stop Desktop from writing back a stale SEC email over one changed with `sec config`.
5. Add `sec config ai list/add/edit/use/remove/enable/disable` to manage AI model profiles.
6. Add `sec config ai set-key/unset-key` to store AI API keys encrypted.
7. Add the `llm` package with Anthropic and OpenAI-compatible model clients.
8. Add `sec config ai test` to check a model profile's connection and tool calling.
9. Add a number check that flags AI answer numbers missing from tool results and advice wording.
10. Add the cached valuation explainer in `llm`, shared by the CLI and Desktop.
11. Mark the `llm` abstraction done in TODO.md, with the `--ai` flag tracked under the valuation explanation.
12. Add `sec valuation <code> --explain` with `--ai <profile>` to explain the valuation with an AI model.
13. Show the shared AI model profiles read-only in Desktop settings.
14. Add, edit, delete, set default and enable or disable AI model profiles in Desktop settings.
15. Save and clear encrypted AI API keys in Desktop settings.
16. Share connection and tool-calling probes in `llm` and add connection testing with cancellation and results to Desktop settings.

### v0.4.8

1. Add the AI/LLM design in `docs/ai.md` and its roadmap in `TODO.md`.
2. Add an Agent Skill (`skills/sec/SKILL.md`) so AI agents such as Claude Code can query sec, with setup in `docs/agent-skill.md`.
3. Add `sec mcp`, a read-only MCP server over stdio for AI agents, with a `get_quote` tool and `scripts/mcp-smoke.py` to check it by hand; see `docs/mcp.md`.
4. Share security search and quote change code between `sec quote`, `sec watch` and `sec mcp`.
5. Report no trading for fund NAV quotes. **JSON change:** `sec quote --json` `volume` and `amount` are now `null` instead of 0 when `nav` is `true`.
6. Merge `sec quote` keys that find the same security into one row, with a note. **JSON change:** `--json` returns one row for them instead of one each, and adds `notes`.
7. Return null volume and turnover for fund NAV quotes in the Desktop API. **JSON change:** `/api/v1/quote` and `/api/v1/quotes` return `null` instead of 0 when `nav` is `true`.
8. Shorten the v0.4.0–v0.4.6 CHANGELOG entries to one sentence each.
9. Add the `search_security` tool to `sec mcp`.
10. Add a TODO item to mark indexes in search results in the provider.
11. Share the K-line market mapping between `sec kline` and `sec quote-history`.
12. Add the `get_kline_summary` tool to `sec mcp`.
13. Add a TODO item for proportional price adjustment, since East Money's forward adjustment distorts percentages over long ranges.
14. Add the `get_financials` tool to `sec mcp`.
15. Move the 10-minute SEC company facts cache from Desktop to `provider/edgar` and use it in `sec mcp`.
16. Move the valuation candidate selection (`000001` is both an index and a stock) from `sec valuation` to the `valuation` package.
17. Add the `get_valuation` tool to `sec mcp`.
18. Add the `list_filings` tool to `sec mcp`.
19. Add the `get_macro_series` tool to `sec mcp`.

### v0.4.7

1. Fix quotes for US OTC stocks such as `$TCEHY`.
2. Retry a failed upstream request once when the connection drops or times out.
3. Accept full codes such as `SH600519` or `$AAPL` when Sina search is down.
4. Show US quote times in New York time and add `timezone` to quotes. **JSON change:** US `trade_date` and `time` are now New York time.
5. Add `scripts/live-smoke.sh` to check every command against live data by hand.
6. Find HK codes written with a prefix, such as `hk00700`.
7. Split the Desktop 自选与行情 page into 自选, a full-page watchlist quote table that opens by default, and 研究, the security research page.
8. Rename the Desktop 研究 tabs 概览 to 行情 and 公司 to 概览, with 概览 first.
9. Open the Desktop 研究 page on the 概览 tab.
10. Rank US OTC tickers such as `$TCEHY` after HK and A-share results, so `腾讯` picks `HK00700`.
11. Fill HK `info` with company name, industry, listing date, description, PE and PB from East Money. **JSON change:** HK `category` is now an industry name, and `listing_date` is a date or empty instead of `--`.
12. Show HK company info, PE and PB on the Desktop 概览 tab.
13. Fix quotes for funds such as `易方达蓝筹`: listed ETFs and LOFs use exchange quotes, other funds show the latest NAV.
14. Add fund `info` and a fund 概览 tab in Desktop with NAV, returns and the fund profile.

### v0.4.6

1. Add US stock valuation and dividend yield to `sec valuation` and the Desktop valuation tab, built from Sina prices and SEC XBRL filings.
2. Support US valuation for companies that report shares only per share class, such as Berkshire Hathaway and Visa.
3. update todo and readme
4. Add a one-feature-per-PR rule to `CLAUDE.md` and `AGENTS.md`.
5. Show US financial statements in the company's reporting currency. **JSON change:** `sec financials --json` amounts are in the reporting currency; check `currency`.
6. Support annual financial statements for IFRS filers such as TSMC and SAP.
7. Report US valuation as unsupported for foreign companies instead of returning wrong numbers.
8. Derive US parent stockholders' equity when a company tags it only by dimension.
9. Fill per-class EPS and book value per share in the US valuation table for Berkshire Hathaway and Visa.
10. Document the measured error of Visa's class conversion in US valuation.
11. Add US dividends by fiscal year to `sec info -d`.
12. Add the Desktop dividends tab for US stocks.
13. Add chart analysis to the Desktop US financials tab.
14. Share setup across the Desktop US SEC handlers; financials now returns 404 for companies without XBRL data.
15. Move the Desktop US company and filings handlers onto the shared setup, with unified error messages.
16. Add a button to hide or show the Desktop left navigation.

### v0.4.5

1. Add US company filings from SEC EDGAR: `sec filings` and the Desktop announcements tab, with a contact email in the new `~/.sec/config.json`.
2. Add US support to `sec info` and `sec kline`, and US tickers to Desktop price history and technical analysis.
3. update default email for SEC EDGAR API
4. Add US financial statements from SEC XBRL to `sec financials` and the Desktop company and financials tabs.
5. Fall back to EDGAR full-text search when filings for heavy filers run short.
6. Fix edge cases in US financial statements and filings.
7. Detect accounting-standard changes and support quarterly statements before the first annual filing.
8. Fix Sina security search after new trailing fields appeared in its responses.

### v0.4.4

1. Add dividend yield to `sec valuation` and the Desktop valuation tab.
2. Run `govulncheck` in CI and skip the flaky live `TestQuoteWs`.
3. Add a chart view to the Desktop financials tab.
4. Fix `macro cn financing` for 2017–2019.
5. Fix `macro cn fiscal` for 202406 and 202407.
6. Report partial failures in `sec quote` and `sec financials` text mode and exit non-zero.
7. Add strategy backtests to `sec strategy --backtest` and the Desktop technical analysis tab.
8. Pin the macOS CI and release runners to `macos-15`.
9. Add silver to `sec metal` and fold `sec metal-history` into `sec metal -b/-e`.
10. Add commodity futures: `sec commodity` and a Desktop Commodities page.
11. Share terminal table styling and East Money request helpers across commands.

### v0.4.3

1. Report search failures as errors instead of no results.
2. Check HTTP status for K-line, Sina and gold requests, and fix `MultiSearch` hanging with more than 8 keywords.
3. Use the previous close for PE, PB and market cap when there is no current price.
4. Run `gofmt` and `go vet` in CI, and add a macOS Desktop job.
5. Fall back to Sina daily bars when East Money K-line requests fail.
6. Verify `sec upgrade` downloads by sha256 and replace the binary atomically.
7. Add `--json` to read-only data commands.
8. Add weekly and monthly K-lines through `--freq` in the CLI and Desktop.
9. Fix announcement sizes shown 1024 times too small, and show announcement dates in Beijing time.
10. Fix `sec quote` showing the wrong code when an A-share and an H-share share a name.
11. Reject `sec strategy` periods below 1 and check data length against the longest period.
12. Show HK quote dates as `YYYY-MM-DD`, and return `null` for DCF values that do not apply.
13. Add an industry peer comparison to `sec valuation` and the Desktop valuation tab.
14. Harden Desktop with error boundaries and a `Host` check against DNS rebinding.
15. Upgrade `golang.org/x/net` to v0.62.0 and require Go 1.26.10 for vulnerability fixes.

### v0.4.2

1. Upgrade the Desktop UI dev dependencies Vite and esbuild, and use the official npm registry in `package-lock.json`.
2. Replace the CodeQL default setup with a workflow that excludes `testdata`.
3. Build the macOS Desktop frontend on Node 24 and upgrade `actions/setup-node` to v7.
4. Pin the Ubuntu workflow jobs to `ubuntu-24.04`.
5. Keep only open items in `TODO.md` and move the macro overview to `docs/macro-coverage.md`.
6. Add `macro us treasury` with all 11 yield curve tenors.
7. Rename `balance-sheet` to `financials`, `balance-sheet-download` to `annual-report` and `macro us investment` to `macro us housing-starts`, keeping aliases.
8. Add `docs/commands.md` mapping data commands to Desktop features.
9. Add a Desktop macro page covering all 61 `sec macro` indicators.
10. Add a macro indicator catalog shared by Desktop and `sec macro --help`.
11. Add a Desktop FX page covering all six `sec fx` commands.
12. Show latest prices in the Desktop watchlist and add a watchlist quotes tab.
13. Add MA and Bollinger Band overlays to Desktop candlestick charts.
14. Add a shared `indicator` package for moving averages and Bollinger Bands.
15. Skip unparsable securities in Sina batch quotes.
16. Add insider announcements and an IPO announcement date view to Desktop.
17. Make `sec insider` match titles strictly, deduplicate and fetch past the first 30 results.
18. Show CNINFO announcement dates in Beijing time.
19. Add a Desktop technical analysis tab with MA crossover, MACD, RSI and Bollinger Bands.
20. Move strategy indicators and signal rules into the `indicator` package.
21. Fix a spurious first-day sell signal in `sec strategy boll`.
22. Count zero and negative indicator values as real data in `sec strategy`.
23. Seed the MACD signal line from the first valid MACD value.
24. Report RSI as 50 instead of 100 for completely flat prices.
25. Reject `NaN` for Desktop strategy and Bollinger parameters.
26. Add a Desktop valuation tab for A-shares, sharing a new `valuation` package with `sec valuation`.
27. Rework `sec valuation` on East Money daily valuation data; it now covers A-shares only.
28. Guard the DCF against invalid parameters and loss-making companies.
29. Fix the A-share PE (TTM), and show `-` for unreliable HK and B-share metrics.
30. Remove the always empty basic EPS row from financials.

### v0.4.1

1. Share one watchlist between the CLI and Desktop.
2. Add listing-date filters to Desktop New Listings.
3. Add version details and update checks to Desktop.
4. Refresh the Desktop layout and themes.
5. Simplify the Desktop sidebar brand.
6. Update README with the feature matrix and Desktop screenshots.
7. Add NBS monthly CPI, core CPI and PPI collection.
8. Add `macro cn` price queries with caching.
9. Add `macro cn pmi`.
10. Add `macro cn nonmanufacturing-pmi` (`npmi`).
11. Add `macro cn industrial` (`ip`).
12. Add `macro cn retail`.
13. Add `macro cn unemployment`.
14. Add `macro cn financing` (`afre`).
15. Add `macro cn loans`.
16. Add `macro cn money`.
17. Add `macro cn reverse-repo`.
18. Add `macro cn lpr`.
19. Add `macro cn rrr`.
20. Embed RRR history and update it incrementally.
21. Add `macro cn treasury`.
22. Use pointer requests and results for ChinaBond Treasury collection.
23. Restore the ChinaBond historyQuery collector and show three- and six-month yields.
24. Add `macro cn gdp`.
25. Add `macro us cpi` and `macro us core-cpi`.
26. Add `macro us pce` and `macro us core-pce`.
27. Add `macro us payrolls`.
28. Fix payroll change baselines and archive validation.
29. Add `macro us earnings` and `macro us weekly-hours`.
30. Add `macro us unemployment` and `macro us participation`.
31. Add `macro us initial-claims` and `macro us continued-claims`.
32. Add `macro us real-pce`.
33. Add `macro us real-income`.
34. Add `macro us industrial-production` and `macro us capacity-utilization`.
35. Add `macro us effr`.
36. Add `macro us target-rate`.
37. Add `macro us gdp`.
38. Add `macro us money` (`m2`).
39. Add `macro cn investment`.
40. Add `macro cn property`.
41. Add `macro us trade` and `macro us real-trade`.
42. Validate NBS investment and property data before caching.
43. Add Chinese industrial enterprise profits, inventories and receivables.
44. Add US housing starts, building permits and equipment investment.
45. Add US new and existing home sales and the FHFA house price index.
46. Simplify the Chinese industrial enterprise terminal table.
47. Add US corporate profits, business inventories and manufacturing new orders.
48. Add US bank credit, lending standards and household debt service ratios.
49. Add Chinese household per-capita income and consumption.
50. Add Chinese household RMB deposits.
51. Add Chinese government budget and fund receipts and expenditures.
52. Add US monthly federal receipts, outlays and surplus or deficit.
53. Add `macro cn trade` and `macro cn reverse-repo-history`, and extend the RRR archive to 2018.
54. Fix September 2019 RRR cuts and keep trade data when partner data is missing.
55. Add `fx quote`, `fx history`, `fx fixing` and `fx reference`.
56. Map CFETS central parity values by returned currency columns.
57. Add derived FX cross rates and CNY/CNH spreads.
58. Add Chinese services trade and CNY goods-trade partner reports.
59. Add `macro us debt`.
60. Add `macro us auctions`.
61. Add `macro us debt-cash`.
62. Add `--standard-json` for Chinese M1/M2 and US M2.
63. Extend `--standard-json` to Chinese retail sales and property.
64. Extend `--standard-json` to US GDP and EFFR.
65. Extend `--standard-json` to US CPI and jobless claims.
66. Extend `--standard-json` to PCE and real consumption.
67. Extend `--standard-json` to real income, industrial production and capacity utilization.
68. Extend `--standard-json` to US labor market series.
69. Extend `--standard-json` to US fiscal flows, profits, inventories and orders.
70. Extend `--standard-json` to US credit, housing and investment series.
71. Drop per-row diagnostics from `--standard-json` and share one periodic adapter.
72. Extend `--standard-json` to US payrolls, home sales, house prices and trade.
73. Extend `--standard-json` to the US federal funds target range.
74. Extend `--standard-json` to US Treasury debt, cash flows and auctions.
75. Extend `--standard-json` to Chinese PMI, industrial value added and unemployment.
76. Extend `--standard-json` to Chinese GDP, household income, investment and industrial enterprises.
77. Extend `--standard-json` to Chinese social financing, loans and deposits.
78. Extend `--standard-json` to Chinese fiscal data and trade.
79. Extend `--standard-json` to Chinese LPR, reverse repo, RRR and Treasury yields.
80. Extend `--standard-json` to Chinese CPI, core CPI and PPI.

### v0.4.0

1. Desktop 新增「黄金行情」，展示上海黄金交易所 Au99.99 最新日行情
2. 黄金行情支持日期范围、快捷查询和历史日 K 线
3. 新增黄金行情 API，K 线支持键盘选择和横向滚动
4. 修复黄金 K 线长区间滚动后价格刻度不可见
5. 「自选与行情」的证券历史改用带成交量的日 K 线
6. Add Desktop Treasury yields.
7. Keep missing Treasury yields as null and reject incomplete annual responses.
8. Add industry, PE, PB and market cap to Desktop overview and company pages.
9. Add Desktop financial statement exports and annual report PDF downloads.
10. Move Desktop security search into the Stocks and Watchlist page.
11. Add date filters and adjustment choices to Desktop security history.
12. Add Desktop IPO prospectus lookup and downloads.
13. Add periodic report filters to Desktop announcements.
14. Add Desktop annual report year-range queries and batch downloads.
15. Expand Desktop Annual Reports into Periodic Reports.
16. Use Lucide icons in the Desktop sidebar.
17. Use the sec logo and SEC branding in the Desktop header.

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
