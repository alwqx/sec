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
