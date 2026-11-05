# sec Desktop 设计文档

## 背景与目标

CLI 版 `sec` 已基本完善。本文档记录桌面版（Desktop）的设计，目标是为 `sec` 提供图形化界面，用可视化的方式查询证券行情。实现参考 [Ollama Desktop](https://github.com/ollama/ollama/tree/main/app) 的架构。

## 调研结论：Ollama Desktop 架构

Ollama 桌面版**不是 Electron**，而是「纯 Go 外壳 + 系统 WebView + React 前端」：

| 层       | 实现                                                                                                                                                                |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 桌面外壳 | Go 二进制，内嵌系统 WebView（macOS `WKWebView` / Windows `WebView2`），基于 vendored 的 `webview/webview_go`（CGO 胶水），构建标签 `//go:build windows \|\| darwin` |
| 后端     | 同一进程内起本地 HTTP server（`127.0.0.1`，生产随机端口 / dev 固定 `3001`），暴露 REST JSON API                                                                     |
| 前端     | React + TypeScript + Vite + Tailwind + TanStack，构建产物 `//go:embed` 打进二进制；dev 模式从 Vite dev server（5173）热加载                                         |
| 通信     | 前端 `fetch` 调本地 API；server 校验 cookie token 防止本机其他程序访问                                                                                              |
| 持久化   | SQLite（`mattn/go-sqlite3`）+ schema 迁移                                                                                                                           |
| 构建     | `go generate ./...` 内 `npm run build` 构建前端                                                                                                                     |

关键结论：**前端是纯静态文件，运行时不依赖 Node**；数据全部由 Go 的 provider 层抓取后经 JSON API 吐出。

## 技术选型

- **外壳**：Go + 系统 WebView。本实现直接依赖上游 `github.com/webview/webview_go` module（自包含、MIT、与 Ollama vendored 的 fork 同源、API 一致），不 vendor 源码。若后续 macOS 兼容性出问题，可改 vendor Ollama 维护的 fork。
- **后端**：本地 HTTP JSON API，直接复用 `provider/` 层（其函数已返回类型化 struct，天然可 JSON 序列化，无需复用 `cmd/` 里面向终端的 table 渲染）。
- **前端**：React + TypeScript + Vite；MVP 用手写 CSS，不引入 Tailwind，降低构建复杂度。
- **持久化**：工作台首版采用 JSON 原子替换，两端自选共用 `~/.sec/watchlist.json`，桌面偏好保存在 `~/.sec/desktop/workspace.json`；后续需要多组合与历史查询时再迁移 SQLite。
- **平台**：macOS 优先；Windows 打包放 Phase 3。

## 财报导出与年度报告

个股「财报」页分为「财务报表」「图表分析」与「定期报告」。

「图表分析」用东方财富 F10 的完整财务报表（约 200 个科目，比数据中心的汇总表多出研发费用、银行收入构成等），可切换最近 10 个年度或 12 个单季（单季时利润表和现金流量表是该季度的值，资产负债表是季末值）：

- 利润构成桑基图：一般企业从营业总收入经毛利、营业利润、净利润流到归母净利润；银行、证券和保险从各项收入汇成营业收入再往下。利润为负或某一步支出超过收入时无法用流量表示，只显示瀑布图。
- 利润瀑布图和现金瀑布图：逐项展示从收入到归母净利润、从期初现金到期末现金的变化，亏损也能画。
- 趋势：收入与利润、利润率、费用率（金融企业为成本收入比）、资产负债率、资产构成（一般企业和银行）、三类现金流、经营现金流 ÷ 净利润，附数据表。

每一步都和报表里的合计科目对齐：没有单列或占收入不到 0.5% 的科目合成“其他”，营业外收支、所得税和少数股东损益再小也单列。公司类型按东方财富行业判断。保险公司的保险服务费用等主要支出上游没有拆分，合在“其他营业支出”里。F10 接口短时间请求过多会断开连接，后端限制并发并重试一次，任一张报表失败都整体报错。

财务报表可按当前报表类型和报告期重新查询，导出 CSV 或 JSON，最多 50 期，保留全部原始字段和数值单位（不按页面的亿元缩放）。CSV 使用 UTF-8 BOM，JSON 包含证券信息、报告日期及原始字段。

定期报告支持年报、半年报、一季报和三季报，按报告年度查询，例如 2025 年度报告通常在 2026 年披露；年报支持 1990 年至上一年度、其余类型支持至本年度的起止年份筛选、分页查询及 PDF 原文下载；起止相同可查单年度。支持单份下载、本页全选与多选顺序下载，展示当前文件、总份数、成功／失败数量与失败原因。翻页清空选择，下载期间禁止翻页和刷新；重新查询或离开栏目取消未完成任务，已保存文件保留，排除摘要、英文、更正与修订版本。文件保存至设置中配置的下载目录（默认 `~/Downloads`，支持绝对路径或 `~/` 开头），成功后显示完整路径，可打开下载目录。同名文件自动追加编号，下载失败或取消清理临时文件。PDF 仅允许巨潮资讯 HTTPS 原文地址，最大 128 MB。

## 架构

```
┌───────────────────────────────────┐
│         桌面进程 (Go)              │
│  ┌──────────────────────────────┐ │
│  │  WebView（系统，WKWebView）    │ │ ← 加载 http://127.0.0.1:<port>/
│  │  React SPA                   │ │
│  └──────────────┬───────────────┘ │
│                 │ fetch /api/*     │
│  ┌──────────────▼───────────────┐ │
│  │  本地 HTTP Server             │ │ ← token cookie 鉴权
│  │   GET /api/v1/search          │ │
│  │   GET /api/v1/quote           │ │
│  │   GET /  （SPA 静态资源）      │ │
│  └──────────────┬───────────────┘ │
│                 │                 │
│  ┌──────────────▼───────────────┐ │
│  │  provider/（复用）            │ │ ← sina.Search / sina.QuerySecQuote
│  └──────────────────────────────┘ │
└───────────────────────────────────┘
```

## 目录结构

```
desktop/
  doc.go       # package main 文档；无构建标签，保证 linux CI 下 go test ./... 不报
               # 「build constraints exclude all Go files」
  main.go      # 入口：监听端口 → 启动 server → 创建 WebView → Run（windows||darwin）
  server.go    # HTTP API + SPA 静态服务 + token 鉴权 + CORS（windows||darwin）
  embed.go     # //go:embed ui/dist（windows||darwin）
  server_test.go  # API handler 单测，gock mock sina（windows||darwin）
  ui/          # React 前端源码（Vite，非 Go 包）
    dist/      # 构建产物（index.html、assets/ gitignore；placeholder.txt 占位入库，保证 go:embed 始终成功）
```

CLI 的根 `main.go` 保持不变；桌面版是独立二进制：`go build ./desktop`。

## API 设计

| 方法 | 路径                          | 说明                                                                      |
| ---- | ----------------------------- | ------------------------------------------------------------------------- |
| GET  | `/api/v1/search?key=<关键字>` | 搜索证券，返回 `{"results":[{name,code,ex_code,security_type,exchange}]}` |
| GET  | `/api/v1/quote?code=<exCode>` | 查询单只证券行情，返回 `SecurityQuote`                                    |

错误统一返回 `{"error": "<msg>"}` + 对应状态码。

## 鉴权

- 启动时用 `crypto/rand` 生成随机 token；server 在返回页面时通过 `Set-Cookie: token=...; HttpOnly` 下发。
- `/api/*` 校验 cookie 与 token 一致，否则返回 403，防止本机其他程序（如网页）跨站调用。
- 所有请求校验 `Host` 必须是 `127.0.0.1` 或 `localhost`，否则返回 403，页面也不下发 cookie，防止 DNS 重绑定。dev 模式也校验。
- 页面内容渲染出错时只影响所在区域，显示提示和重试按钮，侧栏和其他页面不受影响；侧栏等应用外壳出错时显示整个应用的提示和重试按钮。
- dev 模式跳过鉴权并打开 CORS，便于前端连 Vite dev server。

## 研究工作台

### 布局与视觉规范

参考 [CC Switch Desktop 截图](https://github.com/farion1231/cc-switch#screenshots) 的侧栏、紧凑页头与内容分区。SEC 品牌位于约 200px 宽的全高侧栏顶部，设置固定在导航底部；右侧约 64px 页头显示页面图标、标题与页面操作，内容区独立滚动。IPO、贵金属与美债的刷新操作通过 `PageActions` 显示在统一页头，状态仍由各业务组件管理。保留系统标题栏，不绘制模拟窗口按钮。

自选与行情使用约 240px 的证券列表与可伸展研究区。搜索仅位于证券列表，刷新自选采用带文字无障碍名称的图标按钮。减少概览重复标题和装饰边框，行情摘要、公司指标、K 线依次展示；研究区与列表分别滚动。窗口宽度不超过 1000px 时改为上下排列，窄窗口导航横向滚动；切换页面回到内容顶部。财报长表格限制局部滚动高度，固定表头与第一列，保留横向查看。

浅色采用白色内容面板、浅灰侧栏与轻边框，深色采用中性炭灰，使用蓝色表示操作和选中数据；红涨绿跌仅用于行情。统一控件约 8px 圆角、面板 12px 圆角及 16/24px 间距，数据使用等宽数字。分段标签采用中性底色与浮起的选中项，导航选中采用中性强调背景。保留已有深色／浅色／跟随系统偏好；尊重减少动画的系统设置。

公告范围、类别和标题查询放在同一内容面板；贵金属、美债按最新摘要、历史查询、图表和明细排列。设置按行情与外观、下载及关于与更新分组。

### 页面与导航

- 「自选与行情」证券列表上方提供证券搜索，支持名称、代码与拼音；搜索保留当前研究证券，查询结果显示在列表中，其他页面不显示证券搜索框。
- 导航图标使用 `lucide-react` 静态导入的 SVG：自选与行情 ChartCandlestick、IPO FilePlusCorner、公告中心 Newspaper、宏观 Landmark、汇率 ArrowLeftRight、贵金属 Coins、大宗商品 Factory、设置 Settings。统一 20px、1.75px 描边，使用 currentColor 跟随主题和选中状态；保留文字标签，装饰图标不进入无障碍名称。
- 左侧全局导航：自选与行情、IPO、公告中心、宏观、汇率、贵金属、大宗商品、设置。页头最左侧的按钮（PanelLeftClose / PanelLeftOpen 图标）隐藏或显示左侧导航，隐藏后内容区占满窗口宽度，按钮留在页头，再点一次展开；鼠标悬停或键盘聚焦时立即提示"点击隐藏菜单"或"点击展开菜单"（自绘提示，不用 `title`，系统 WebView 不一定显示原生提示）。状态存在工作区设置（`workspace.json` 的 `sidebar_hidden`，旧文件没有这一项时按展开处理），点击时立即切换、在后台保存，下次打开保持；不用 `localStorage`，因为 Desktop 每次启动监听的端口不同，浏览器存储按端口隔离。设置页保存时保留当前的菜单状态。窄窗口的横排导航同样可以隐藏。
- 自选与行情：中间切换「我的自选 / 搜索结果」，自选列表显示最新价和涨跌幅；右侧围绕选中证券展示「概览 / 公司 / 财报 / 分红 / 公告 / 技术分析 / 估值 / 自选行情」。「自选行情」是全部自选证券的行情表，勾选后可只看选中的几只。行情刷新失败时保留上一次结果并在列表和栏目中提示；单只证券不可用时标记该行，不影响其他证券。上一次请求未返回时跳过这一轮刷新。切换全局页面保留当前证券与个股栏目。
- 概览：实时行情、可选择日期范围与复权方式的日 K 线蜡烛图（开高低收）、成交量副图与所选交易日的成交额。概览页按偏好轮询行情，自选列表和「自选行情」栏目可见时也按同一间隔轮询，其他页面不轮询；历史走势默认近 3 个月、不复权，支持近 1／3／6 个月、近 1 年及自定义日期（1990 年至上海今日，单次最多 5 年），支持不复权／前复权／后复权；查询后显示已生效条件，复权不改变实时行情报价。K 线可叠加 MA5／10／20／60 和 BOLL(20,2)：指标由后端按所选复权方式计算（共用 `indicator` 包），并多取查询起点之前的数据，所以区间第一天也有完整窗口；上市时间不足的日期为空值，线段断开。指标随行情一次取回，切换时不重新请求。历史走势进入概览或提交条件时加载。切换证券与页面取消旧请求。
- 估值：沪深北 A 股和美股，展示 PE、PB、PS 的历史走势和分位（A 股 2018 年以来，美股最近 10 年，美股由 SEC XBRL 和不复权日线计算、没有同行业对比）、同行业对比、股息率和分红、PEG、ROE、格雷厄姆数、规则汇总和可调参数的 DCF，口径见[估值文档](valuation.md#七sec-valuation-口径)，计算在 `valuation` 包，CLI 与 Desktop 共用。
- 历史行情备用数据：东方财富日线请求失败时，沪深不复权行情（走势图和技术分析）改用新浪日线，页面会提示，成交额显示“—”。新浪只有最近 1023 根不复权 K 线（日线约 4 年），覆盖不了请求区间时提示缩短区间；预热数据尽力而为，区间开头的指标可能受影响；美股不复权日线改用新浪美股日线（从上市起全部历史，不复权，拆股当天价格会跳变，只有日线）；复权和港股行情没有备用来源。新浪搜索把美股都标成纳斯达克，东方财富按纳斯达克（105）查不到时依次改用纽交所（106）和 NYSE American（107）。
- K 线周期：走势图和技术分析都可以切换日线、周线、月线，单次最长分别是 5、20、40 年。周线和月线的日期是该周期最后一个交易日，当前周期还没结束时是未走完的数据。
- 技术分析：沪深证券与港股，可选双均线、MACD、RSI、布林带，参数可调，默认近 1 年、前复权（避免除权缺口产生假信号；`sec strategy` 默认使用不复权价格，加 `--backtest` 时使用前复权，结果可能不同）。图上标出买卖信号，下方列出信号明细。算法和信号规则在 `indicator` 包，CLI 与 Desktop 共用。后端多取查询起点之前的数据做预热（至少一年，长周期按周期放大），预热数据和预热期的信号不返回；新上市证券没有足够历史时，EMA 和 RSI 的值仍受起点影响。信号由规则机械生成，信号表未计入成本，不构成投资建议。
- 回测：沪深 A 股选日线和前复权或后复权时，在信号下方按信号回测，规则和 `sec strategy --backtest` 相同（共用 `backtest` 包，预热长度也和 CLI 一致）。可调初始资金、佣金、最低佣金、印花税和滑点，显示策略和买入持有的总收益、年化收益、最大回撤、期末资产，资产曲线（底部标出持仓区间）、成交明细和没有成交的信号。港股、指数和基金、周线月线、不复权时说明不能回测的原因；复权价格出现非正数时也会说明；回测参数有误时只在回测区块提示，指标和信号照常显示。
- 公司、财报、分红、个股公告：沪深北 A 股都支持；美股的公司、财报、分红和公告见下方「美股」；财报支持资产负债表、利润表、现金流量表与报告期筛选，金额单位亿元，并有桑基图、瀑布图和趋势图。历史走势图和技术分析支持沪深证券、港股和美股（美股价格单位美元，回测只支持 A 股个股）；美股的公告标签页是 SEC 申报，估值栏目含股息率，分红栏目是 SEC XBRL 年报的按财年分红表。
- IPO：分为「新股上市」「发行公告日期」与「招股说明书」三个栏目。「发行公告日期」按公告日期列出巨潮的 IPO 相关公告，可按公告日期起止筛选（可只填一边）并进入个股研究；公告日期不是申购日或上市日，不能当作申购日历。「新股上市」用表格展示东方财富上市信息，支持上市日期起止筛选（可只填一边、包含边界）、重置、筛选后分页和进入个股研究；查询或重置回到第一页，刷新与翻页使用已提交条件。有日期条件时排除日期待定记录，先过滤完整源列表再分页，计数为筛选后总数；无条件时保留原源分页及日期待定记录。CLI 与 Desktop 共用 `eastmoney.ListIPOFiltered`，不完整源响应返回错误，避免显示不完整的筛选结果；合并原有上市排期与新股列表，避免同一数据重复展示。「招股说明书」栏目，支持六位沪深北证券代码（可带 SH/SZ/BJ 前缀）、可选公告起止日期及分页；新股列表可点击「招股书」带入代码。复用 CLI 的首次公开发行及上市分类，包含招股说明书、招股意向书和发行公告，仅展示有效 PDF。支持原文查看、本页全选、单份／多选顺序下载，显示进度和各文件失败信息；取消保留已完成文件，重新查询或离开栏目取消未完成请求。下载复用设置目录、同名编号、临时文件清理及巨潮 HTTPS PDF 校验（每份最大 128 MB）。
- 公告中心：巨潮资讯最新披露 / 当前 A 股证券，可按标题关键字与公告类别（全部／年报／中报／一季报／三季报，选中证券时另有增减持公告）组合查询并分页，切换类别回到第一页并保留已提交关键字；同样适用于个股公告栏目。仅展示有效公告；标题按纯文本呈现，原文链接只允许巨潮资讯域名。桌面通过 Go 绑定在系统浏览器打开原文；开发浏览器直接打开新标签，并提供复制链接。
- 设置：行情刷新间隔 5/15/30/60 秒，深色、浅色和跟随系统外观。窄窗口将导航横排、证券列表与详情上下排列；个股栏目支持方向键、Home、End 切换。

### 贵金属

「贵金属」复用 CLI `metal` 的 `provider/metal`，提供上海黄金交易所 Au99.99 黄金和 Ag99.99 白银，页面顶部切换品种，默认 Au99.99。最新摘要展示截至上海时区今日的最近可用日行情，包含交易日期、收盘、开盘、最高、最低、昨收、涨跌额与涨跌幅，单位随品种（黄金元/克，白银元/千克）；它是日行情而非盘中报价，休市时保留最近交易日。

上海黄金交易所的访问频率限制很低，短时间内请求十几次就会被拦截约 10 分钟。Desktop 后端把每个品种的全量日行情缓存 10 分钟，切换品种、修改区间都复用缓存；点「刷新行情」时绕过缓存重新请求。被拦截时提示"访问过于频繁，请稍后再试"。

历史区间默认从上海时区今日向前 30 天，提供近 1 个月 / 3 个月 / 1 年快捷区间与自定义起止日期；以日 K 线蜡烛图和最新在前的明细表呈现；蜡烛实体表示开盘至收盘、影线表示最高至最低，按收盘相对开盘红涨绿跌、平盘灰色，支持悬停/点击查看开高低收与方向键选择交易日，长区间横向滚动且左侧价格轴保持固定可见，明细每页 30 条。单次接口请求复用上游全量日行情，同时返回最新记录与过滤后的历史，避免为最新、历史重复抓取。修改历史范围不会把顶部最新行情替换成区间末日，首个历史交易日的昨收也保留区间外基准。

页面提供手动刷新、请求取消、失败重试、日期错误提示与空数据状态；不按股票行情间隔轮询。没有有效昨收时，昨收、涨跌额和涨跌幅返回 `null` 并显示「—」，不将缺失值解释为零涨跌。黄金与个股历史共用 `CandlestickChart`，均支持固定价格轴、悬停/点击与键盘选择，以及长区间横向滚动；证券历史保持不复权，沪深证券价格及成交额单位为元、港股为港元；成交量按 provider 归一化结果以股/份显示（沪深已从手转换，不再重复乘 100），成交量副图与价格图共享横轴、固定纵轴和交易日选择。黄金数据源不提供量额，因此黄金页只绘制价格 K 线。

### 大宗商品

「大宗商品」复用 CLI `commodity` 的新浪期货数据（`provider/sina/futures.go`）。上方表格列出 8 个品种的最新行情：国内主力连续合约原油、铜、铁矿石、螺纹钢、豆粕，国际 WTI 原油、布伦特原油、COMEX 铜，包含最新价、涨跌（相对昨日结算价）、开盘、最高、最低、昨结算、单位和行情时间，红涨绿跌；点「刷新行情」重新请求。个别品种没有行情时照常显示其余品种，并在表格下方列出未取到的品种。点击品种名在下方查看它的日 K 线和日行情明细，默认原油、近 3 个月，提供近 3 个月 / 1 年 / 3 年快捷区间和自定义日期，明细每页 30 条；国内品种显示结算价，涨跌相对前一日结算价，前一日没有结算价时相对收盘价并标 *；国际品种的日行情没有结算价，涨跌相对前一日收盘价，和上方表格略有差别。页面说明主力连续合约换月时价格会跳空。后端把每个品种的全部日行情缓存 10 分钟（WTI 从 1996 年起），切换品种和区间时不重复下载。

### 美股公司与财报

美股（代码以 `$` 开头）的「公司」标签页复用 `usstock` 包：上方是新浪行情和估值（价格、涨跌幅、市盈率和每股收益 TTM、总市值、总股本、52 周区间），下方是 SEC EDGAR 公司资料（名称、CIK、交易所、行业和 SIC、注册地、申报人类别、财年截止日、地址、电话、曾用名）；任一部分失败时照常显示另一部分并说明原因。

「财报」标签页分「财务报表」和「图表分析」。财务报表复用 CLI `financials` 的 SEC XBRL 整理（`provider/edgar/facts.go`），可切换利润表、资产负债表、现金流量表和年度、季度，报告期为列、从新到旧（年度最多 10 期，季度最多 12 期），金额单位亿元（公司的列报币种，非美元时在数据来源后注明），负数标绿，推算值标 `*` 并有说明。XBRL 数据按公司缓存 10 分钟，最多保留 20 家公司，写入时清理过期条目并淘汰最早写入的公司；缓存命中时切换报表和年度、季度无需重新下载。首次年报之前也可读取已披露的 10-Q 季报；按国际会计准则（IFRS）申报或已从 US GAAP 切换为 IFRS 的公司用 IFRS 科目显示年度报表，数据来源后注明 IFRS；这些公司只提交 20-F 年报，切换到季度时给出说明。图表分析和 A 股共用前端组件，数据由 `financials.FromSEC` 用同一份 XBRL 数据整理成和 A 股相同的格式（`source` 为 `sec`，`currency_name` 是列报币种）：利润构成桑基图和瀑布图按营业收入 →（营业成本）毛利 →（研发、销售及管理费用）营业利润 → 税前利润 →（所得税）归母净利润拆解，没有的合计跳过；有净利息收入、没有营业成本和毛利的公司按银行处理，营业收入先拆成净利息收入和非利息收入，再扣信用损失准备和营业费用到税前利润。SEC 数据只有归母净利润，税后到归母净利润之间的少数股东损益、终止经营等合为一项。现金瀑布图从上期末资产负债表的现金经三类现金流到本期末现金，受限现金、汇率变动等差额合为一项；资产构成拆成现金及等价物、其他流动资产和非流动资产（银行只拆出现金）。趋势图有收入与归母净利润、毛利率和净利率、销售及管理费用率和研发费用率、资产负债率（没有负债合计时用总资产减股东权益）、资产构成、三类现金流和经营现金流 ÷ 归母净利润；年度最近 10 个财年、单季最近 12 个季度，按财年标注（比如 FY2026Q1）。

美股的「估值」栏目见[估值文档](valuation.md#美股)，股息率在估值栏目里。

「分红」标签页和 CLI `sec info -d` 相同，复用 `usstock.LoadDividends`（XBRL 数据和财报、估值栏目共用缓存）：按财年从新到旧列出每股宣告股息和同比、支付股息、归母净利润、分红率和回购，金额单位亿元（公司的列报币种），口径和拆股调整见[命令文档](commands.md)。SEC 数据没有除权除息日、登记日和派息日；从不分红也不回购的公司显示"可能不分红"；新浪日线读不到时注明每股股息没有按拆股调整。

### 美股 SEC 申报

美股（代码以 `$` 开头）的「公告」标签页显示 SEC EDGAR 官方申报，复用 CLI `filings` 的 `provider/edgar`：公司名称、CIK、交易所、行业和财年截止日，按「主要申报 / 年报和季报 / 重大事项 / 内部人交易 / 全部」筛选，每页 20 条，显示申报日、类型、报告期和中文说明（8-K 附事项），可在系统浏览器打开原文或复制链接；打开原文只允许 `https://www.sec.gov/Archives/edgar/data/` 下的申报文件，路径须已规范化、不含 `..`。按公司和类别缓存 5 分钟，翻页不重新下载；按类型筛选时最多再读 10 页历史申报凑够 100 条，还不够时用 EDGAR 全文检索按类型补全，全文检索最多 20 页，凑够所需条数即停止；达到上限仍不足或检索失败（包括超时）时保留已有结果，页面提示结果可能不全。共享配置无效时用默认邮箱，不影响工作台。美股的分红栏目暂不支持，股息率见估值栏目。访问 SEC 使用设置 → 数据源中的联系邮箱。

### 公司核心指标

个股概览与公司页均展示行业、市盈率 PE（TTM）、市净率 PB、总市值和流通市值。目前沿用公司资料栏目的沪深北 A 股范围；基金、港股和美股不请求该指标接口。

`GET /api/v1/metrics?code=SH600036` 只请求新浪行情与扩展行情，不依赖公司资料 HTML。通过 `provider/sina.QueryCoreMetrics` 加载，与 CLI `info` 共用计算函数：PB = 当前价 / 每股净资产，PE（TTM）沿用 CLI 的股本及利润字段口径，A 股总/流通市值 = 当前价 × 总/流通股本 × 10000。返回行业、倍数、市值（人民币元）及行情快照日期/时间；界面将市值格式化为万元、亿元、万亿元。亏损或缺少利润数据时 PE 显示「—」，无法计算、零占位及非有限数值也显示「—」，不误报为 0 倍或 0 元；可计算的负 PB 保留。

概览中核心指标每次请求完成后等待 60 秒刷新，可手动刷新；公司页按进入时加载并提供手动刷新。核心指标、公司资料和实时行情分别加载、分别重试，一项失败不会阻塞其他内容。自动刷新失败保留并标明上次成功快照；切换证券或页面时取消请求。指标快照时间与实时行情刷新周期分别标注。

### 宏观

「宏观」覆盖 `sec macro` 的全部 61 个指标。页面先切换中国／美国，再按 10 个主题分类选择指标；指标清单在 `cmd/macro/catalog.go`，`--help` 分组和页面菜单共用，测试保证它与实际命令一致。

Desktop 不为每个指标单独取数：`macro.Export` 在进程内运行命令的 `--standard-json`，所有指标共用缓存、校验和统一格式。`region`、`command`、选项取值和日期格式必须符合清单，否则返回 400，不会把任意参数传给命令。覆盖范围、上游失败等命令错误返回 502，错误信息直接显示给用户。缓存写入都是临时文件加重命名，并发查询不会写坏文件。

```text
GET /api/v1/macro/catalog
GET /api/v1/macro/data?region=cn&command=cpi&begin=202601&end=202609
```

`begin`、`end` 使用命令的格式（YYYYMM、YYYYQ1、YYYYMMDD），留空时使用命令的默认窗口。`refresh=true` 重新采集，没有缓存的命令忽略。响应是统一导出格式，去掉了体积较大的 `original`。

序列数量差别很大（`cn trade` 有 36 条，`us debt` 有 75 条，选项可能再改变序列），所以页面不做透视表，而是：

- 序列列表显示最新值、单位、所属期及官方／计算口径，超过 8 条时可筛选。查询窗口内没有观测的序列不列出。
- 选中序列后显示走势和逐期明细。缺失值显示 `—` 和原因（如“官方未公布”“1月免报”），不补值；图表保留断点。重新查询时保留选中序列和筛选文字。
- 事件型指标（国债拍卖、存款准备金率，以及同一日期有多条记录的序列）只显示明细和事件范围或 CUSIP，不绘制走势，同一最新日期有多条记录时最新值显示“多个事件”。
- 月份用文本框输入（WKWebView 不支持 `type="month"`），提交前校验格式。七天逆回购中标利率历史截至 2024-07-19，快捷区间从这个日期往前推算。

`us treasury` 仍使用下面的美债页面。`us target-rate` 的默认窗口经常没有调整事件，首次打开从 20081216 查询。

依赖 `original` 的内容不展示，例如 `macro us gdp --vintages` 的修订档案和目标区间的当前有效值。序列名称来自 `definition` 和口径，部分美国序列（如 `us debt`）的名称保留英文原文。

### 汇率

「汇率」覆盖 `sec fx` 的 6 个命令，每个命令一个栏目。与宏观相同，`fx.Export` 在进程内运行命令的 `--json`，保留来源、价格类型和时间口径。详细口径见[汇率文档](forex.md)。

```text
GET /api/v1/fx/options
GET /api/v1/fx/data?command=fixing&begin=20260101&end=20260131
```

`command` 可选 `quote`、`fixing`、`reference`、`history`、`cross`、`spread`；其余参数是 `source`、`pair`、`at`、`max_skew`、`max_age`、`refresh`。与所选命令和来源无关的参数不会传给命令，例如新浪交叉汇率不带日期。取值或日期格式无效返回 400，覆盖范围和上游失败等命令错误返回 502。

页面按数据形态展示：

- 最新行情和新浪交叉汇率是报价：逐行显示数值和观测时间，交叉汇率另外显示公式和原始输入。
- 中间价、参考汇率、历史日线和 FRED、CFETS 交叉汇率是日度序列：切换货币对后显示走势和明细，交叉汇率的明细带每天的公式输入。
- 价差显示绝对价差、点差、相对价差和隐含 CNH/CNY，以及原始输入。

报价过期或时间差超限时不计算，页面显示 `—` 和原因。休市时报价常常超过 24 小时，这时放宽报价年龄上限也不会有结果。

### 美债收益率

位于「宏观 → 美国 → 利率与收益率」。

「美债收益率」复用 CLI `bond` / `bond-history` 的美国财政部年度 XML 数据，展示 1/3/6 个月、1/2/3/5/7/10/20/30 年共 11 个期限。最新交易日独立于历史查询区间；日期使用 `America/New_York` 时区，日收益率单位为百分比，10 年期日变动为 bp（1 bp = 0.01 个百分点）。红绿仅表示收益率上升/下降，不表示债券价格涨跌。

页面提供最新期限明细、可切换日期的收益率曲线、可切换期限的历史折线与每页 30 条明细。收益率曲线横轴按期限等距排列，历史横轴按交易日顺序排列；选择历史期限后，修改日期范围仍保留该期限。默认最近 30 天，支持近 1 个月 / 3 个月 / 1 年快捷区间与自定义日期；允许 1990 年至美东今日，单次历史范围最多 5 年，避免一次抓取过多年度数据。提供手动刷新、请求取消、失败重试与空区间状态，不进行盘中轮询。

`GET /api/v1/bond?begin=2026-01-01&end=2026-01-31` 返回 `tenors`、`latest`、`results` 及实际起止日期。接口只抓取历史区间、前值缓冲和最新行情所需年份的并集，每年一次；补读开始日期之前 35 天所在年份，保留跨年首日的日变动基准。任一年查询失败返回 502，不将不完整历史显示为查询成功；不连续年份之间也不会计算虚假的日变动。

provider 在保持原有数值字段的同时，通过 `MissingTenors` 记录上游省略、空值或 `m:null` 的期限，桌面以 `null` /「—」呈现并在折线中断开；真实 0% 仍是有效数值。没有有效前一交易日基准时，前值与 bp 变动也显示「—」。

### 自选与设置持久化

自选通过「加入自选 / 已加入自选」按钮增删，CLI 与 Desktop 共享 `watchlist` 包和 `~/.sec/watchlist.json`。共享记录包含代码、带市场前缀标识、名称、证券类型、交易所及添加时间；按标准标识去重，最多新增到 500 只。旧数据超过上限时不截断，仍允许删除。跨进程操作使用 `gofrs/flock` 文件锁，读改写在同一锁内完成，以同目录临时文件、同步写入和原子替换发布。锁文件保留，进程退出由操作系统释放锁。

首次访问共享存储时合并旧 CLI 数组与 Desktop 自选，原文件生成同目录 `*.pre-shared-*.bak` 备份；版本化共享文档作为一次性迁移标记，随后删除 workspace 的旧 watchlist 字段。中断重试不会把已删除证券重新导入。迁移不依赖网络；旧 CLI 缺少类型时按代码补齐常见基金类型，其余默认股票，重复记录优先保留 Desktop 的完整证券信息及 CLI 原有添加时间。损坏文件返回错误并保留原文。旧版 CLI 不能读取新格式，两端应同步使用新版。

设置仍保存到 `~/.sec/desktop/workspace.json`，仅持久化 settings；SEC 联系邮箱例外，保存在和 CLI 共用的 `~/.sec/config.json`（`config` 包，文件锁加原子写入），工作台接口返回时合并进 settings，保存时留空表示不修改，不写入 workspace.json。保存设置即使携带过期 watchlist 快照也不会改写共享自选；增删使用独立接口，只提交对应证券。首次保存设置前先完成旧列表迁移，设置文件与迁移使用同一工作台文件锁。Desktop 返回「自选与行情」、重新获得窗口焦点或点击「刷新自选」时同步列表，后台响应不会覆盖本窗口正在进行的增删。当前研究证券、筛选条件与菜单仅在本次运行中保留。

### 版本与更新

设置页「关于 SEC Desktop」展示链接器注入的版本、构建时间、提交号及当前平台；未注入版本时显示开发构建，其他缺失字段显示未知。仅点击「检查更新」时查询 GitHub 最新正式 Release，CLI 与 Desktop 共用 `release.FetchLatest`。版本按语义版本比较，区分可更新、已是最新、当前版本更高及无法比较；查询失败时保留直接访问 Release 的入口。

macOS 按当前进程架构匹配 `sec-<tag>-desktop-darwin-<arch>.dmg`，不将 CLI 压缩包作为安装包。无匹配文件时提示用户前往 Release 查看。原生应用通过绑定在系统浏览器打开官方发布页面或安装包链接，开发浏览器使用新标签；下载和安装由用户完成，不替换运行中的应用。链接仅允许 `https://github.com/alwqx/sec/releases/` 下的发布与下载路径。

### 新增 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/version` | 当前版本、构建时间、提交号、平台与官方 Release 地址，无外部网络请求 |
| GET | `/api/v1/updates` | 手动查询最新正式版本、比较状态及匹配当前平台的 Desktop 安装包；GitHub 错误返回 502 |
| GET | `/api/v1/watchlist` | 读取 CLI／Desktop 共享自选，返回 results |
| POST | `/api/v1/watchlist` | JSON action=add 与 security，或 action=remove 与 code；按最新列表执行单项增删，要求鉴权与同源 |
| GET | `/api/v1/workspace` | 本地自选与设置；无文件时返回默认值 |
| PUT | `/api/v1/workspace` | 仅保存 settings 并返回最新共享自选与设置，不应用请求中的 watchlist 快照；要求 JSON、有效参数及生产同源请求 |
| GET | `/api/v1/metrics?code=SH600036` | 沪深北 A 股行业、PE（TTM）、PB、总市值、流通市值与快照时间；未知数值为 null |
| GET | `/api/v1/company?code=SH600036` | 公司资料 |
| GET | `/api/v1/dividends?code=SH600036` | 分红记录，按每 10 股展示税前派息/送转 |
| POST | `/api/v1/financials/export` | JSON 参数 code/report/period/format，format 为 csv/json；返回本地文件路径及大小 |
| GET | `/api/v1/annual-reports?code=SH600036&start_year=2022&end_year=2025&page=1` | 按报告年度范围查询正文，兼容 year 单年度参数；范围参数必须成对提供，type 为 annual（默认）/halfyear/q1/q3 |
| POST | `/api/v1/annual-reports/download` | JSON 参数 code/year（字符串）/url/type（默认 annual）；下载定期报告 PDF 并返回本地文件路径及大小 |
| GET | `/api/v1/financials/charts?code=SH600036&freq=annual` | 财报图表数据；freq 为 annual（最近 10 个年度）或 quarter（最近 12 个单季） |
| GET | `/api/v1/financials?code=SH600036&report=balance&period=annual` | 财报；report 为 balance/income/cashflow，period 可省略或 annual/halfyear/q1/q3 |
| GET | `/api/v1/strategy?code=SH600036&kind=macd&begin=2026-01-01&end=2026-10-30&fq=qfq` | 技术指标序列和信号；`kind` 为 ma、macd、rsi、boll，参数 fast、slow、signal、period、overbought、oversold、k 可选；回测参数 cash、commission、min_commission、stamp_tax、slippage 可选（费率以 % 为单位），结果在 `backtest`，不能回测时原因在 `backtest_note` |
| GET | `/api/v1/quotes?codes=SH600036,HK00700` | 批量行情，最多 1000 只，后端每 100 只分批查询；不可用的代码列在 `missing` |
| GET | `/api/v1/announcements?code=SH600036&type=insider` | 当前证券标题含增持或减持的公告：两个关键字各查最多 10 页，按标题严格过滤、去重并倒序；不是结构化交易记录，结果被截断时 `truncated` 为 true |
| GET | `/api/v1/ipo/announcements?since=2026-10-01&until=2026-10-31` | 按公告日期查询 IPO 相关公告，只填一端时补齐另一端 |
| GET | `/api/v1/fx/options` | 交叉汇率各来源支持的货币对 |
| GET | `/api/v1/fx/data?command=fixing` | 汇率命令的 JSON 结果，详见「汇率」 |
| GET | `/api/v1/macro/catalog` | 宏观主题分类和指标清单 |
| GET | `/api/v1/macro/data?region=cn&command=cpi` | 单个宏观指标的统一格式数据，详见「宏观」 |
| GET | `/api/v1/bond?begin=2026-01-01&end=2026-01-31` | 美国国债最新各期限收益率与区间历史；起止日期 YYYY-MM-DD，默认美东今日向前 30 天，单次最多 5 年 |
| GET | `/api/v1/metal?inst=Au99.99&begin=2026-01-01&end=2026-01-31` | 贵金属最新日行情与区间历史，同时返回品种列表 `instruments`；`inst` 为 Au99.99 或 Ag99.99，也可用简写 `au999`、`ag999`，省略时为 Au99.99；日期格式 YYYY-MM-DD，省略 end 时默认上海时区今日，省略 begin 时默认 end 向前 30 天；`refresh=1` 绕过 10 分钟缓存 |
| GET | `/api/v1/commodity` | 8 个大宗商品的最新行情（`results`），字段同 `sec commodity --json`；个别品种失败时其余照常返回，原因在 `failed`，全部失败时 502 |
| GET | `/api/v1/commodity/history?id=cu&begin=2026-01-01&end=2026-01-31` | 一个品种的日行情：`commodity`（品种信息）和 `results`；日期参数同贵金属 |
| GET | `/api/v1/history?code=SH600036&begin=2024-01-01&end=2024-12-31&fq=qfq` | 沪深/港股/美股历史开高低收与量额（date/open/close/high/low/volume/turnover），美股代码如 `$AAPL`；日期为 YYYY-MM-DD，默认近 3 个月，fq 为 bfq/qfq/hfq（默认 bfq），单次最多 5 年 |
| GET | `/api/v1/ipo/prospectuses?code=300750&begin=2018-01-01&end=2018-12-31&page=1` | 招股及 IPO 相关有效 PDF，日期可省略，返回 code/name/results/page/has_more |
| POST | `/api/v1/ipo/prospectuses/download` | JSON 参数 code/url，保存单份 PDF 并返回本地路径；多选由前端逐份调用 |
| GET | `/api/v1/ipo?page=1&since=2024-01-01&until=2024-12-31` | 每页 30 条新股，since/until 为可选上市日期边界（YYYY-MM-DD），返回筛选后的 total 与 has_more |
| GET | `/api/v1/announcements?page=1&code=SH600036&key=年度报告&type=annual` | code 可省略以查询最新披露，支持分页与标题关键字；type 可省略查询全部，或为 annual/halfyear/q1/q3，对应巨潮公告分类 |
| GET | `/api/v1/filings?code=$AAPL&type=periodic&page=1` | 美股 SEC 申报：`company`、`ticker`、`total`、`has_more`、`results`；type 为 periodic（10-K/10-Q/20-F/40-F）、current（8-K/6-K）、insider（Form 3/4/5/144）、all，留空为主要申报；`truncated` 为 true 表示更早的申报没有读取；SEC 没有这家公司时 404 |
| GET | `/api/v1/us/company?code=$AAPL` | 美股公司资料：`info`（`company` 为 SEC EDGAR 公司信息，`quote` 为新浪行情和估值，失败的一方为 null）和 `notes`（失败原因）；两方都失败时 502 |
| GET | `/api/v1/us/dividends?code=$AAPL` | 美股按财年的分红表（`usstock.Dividends`）：`ticker`、`name`、`cik`、`currency`、`currency_name`、`splits`（识别出的拆股）、`splits_unchecked`（日线没读到、没有按拆股调整）和 `years`（`fiscal_year`、`end`、`per_share`、`growth`、`paid`、`net_income`、`payout`、`buyback`，从新到旧）；SEC 没有这家公司时 404 |
| GET | `/api/v1/us/financials/charts?code=$AAPL&freq=annual` | 美股财报的图表数据，格式和 `/api/v1/financials/charts` 相同（`financials.Analysis`），另有 `source`（`sec`）、`currency_name` 和每期的 `label`（财年标注）；freq 为 annual 或 quarter；SEC 没有这家公司或没有利润表时 404 |
| GET | `/api/v1/us/financials?code=$AAPL&statement=income&period=annual` | 美股财报的一张报表：`ticker`、`name`、`cik`、`report`（`standard` 会计准则 US GAAP 或 IFRS，`currency`、`currency_name` 列报币种，`lines` 行定义，`periods` 报告期的 `values` 和推算行 `derived`）；statement 为 income、balance、cashflow，period 为 annual 或 quarter；IFRS 公司只有年度报表，季度返回空的报告期；SEC 没有这家公司或没有 XBRL 数据时 404 |

美股 SEC 接口（公司资料、财报、财报图表、分红、SEC 申报）共用 `desktop/us.go` 的 `serveUS`（公司资料和行情都失败时用自己的 502 提示）；估值接口同时支持 A 股和美股，超时（美股 40 秒）和错误映射（无数据 404、外国公司 422）不同，不用它。`serveUS` 先校验 `$` 代码（400），再设 30 秒超时、用共享配置的联系邮箱访问 SEC，错误统一映射为 400（其他参数无效，说明参数名）、404（SEC 没有这家公司或没有 XBRL 数据，或数据不足以整理并说明原因）、502（上游失败，日志带请求路径）。新增接口统一沿用 token cookie 鉴权。上游失败返回 502；公司资料为空返回 404；参数无效返回 400；列表为空返回 `{"results":[]}`（附带各接口其他元数据）。写入设置额外限制请求体大小、内容类型、同源与有效证券/偏好字段。

## 前端

React 18 + TypeScript + Vite 5，手写 CSS。`desktop/ui/src/components/` 按研究栏目与全局页面拆分，`hooks/useResource.ts` 统一处理数据加载、错误重试与取消。Vite `server.proxy` 将 `/api` 代理到开发模式 Go server（`127.0.0.1:3001`），不依赖浏览器跨域直连。

## 构建与开发

```bash
# 前端（开发热更新 / 生产构建）
cd desktop/ui && npm install && npm run dev        # 开发
cd desktop/ui && npm run build                     # 生产（生成 dist/）
cd desktop/ui && npm test                          # 前端单元测试（需要 Node 24）

# 桌面端
go run ./desktop -dev                        # 联调：WebView 加载 http://localhost:5173，server 固定 3001
go build -o sec-desktop ./desktop            # 正式构建（需先 npm run build 生成 dist）

# macOS：封装带图标的应用（在仓库根目录执行）
bash desktop/package-macos.sh sec-desktop /tmp/sec.app 0.3.14
open /tmp/sec.app
```

应用图标源图为 `desktop/assets/sec-logo.png`，macOS 多尺寸图标为 `desktop/assets/sec.icns`（16–1024 像素，含 Retina）。打包脚本将其放入 `Contents/Resources/`，由 `Info.plist` 的 `CFBundleIconFile` 引用，供 Finder、Dock 和应用切换器显示。直接运行 `go run ./desktop` 或裸二进制不会使用 bundle 图标；本地查看图标请启动打包后的 `.app`。

### 发布（macOS）

`.github/workflows/release.yml` 的 `build-macos-desktop` job 在 `macos-latest`（Apple Silicon，arm64）上：构建前端 → `go build ./desktop` → 组装 `sec.app`（元数据见 `desktop/Info.plist`）→ 对完整应用执行 ad-hoc 签名与严格校验 → `hdiutil` 打包成 `sec-<SEC_VERSION>-desktop-darwin-arm64.dmg`（例如 `sec-v0.3.14-desktop-darwin-arm64.dmg`）→ 校验并挂载镜像验证内部应用签名 → 上传到 Release。文件名中的版本复用构建元数据 `SEC_VERSION`（由 `git describe --tags --always` 生成），与二进制版本保持一致，打包、校验与上传使用同一文件名。所有资源和元数据必须在签名前写入，避免裸二进制的链接器签名与应用包不匹配。

ad-hoc 签名只保证签名完整性，不提供开发者身份，也不等同于 Apple 公证。当前安装包没有 Developer ID 签名与公证，下载后仍可能被 Gatekeeper 拦截；确认来源可信后，可按系统提示在「系统设置 → 隐私与安全性」中选择「仍要打开」。不能保证所有 macOS 版本均允许手动放行。面向普通用户免拦截分发需要 Developer ID Application 证书签名并完成 Apple 公证。

若旧版提示「已损坏」，可先诊断签名：

```bash
codesign --verify --deep --strict --verbose=2 /Applications/sec.app
spctl --assess --type execute --verbose=4 /Applications/sec.app
```

第一条失败说明签名完整性有问题，应更换重新构建的安装包；第一条通过而第二条拒绝，则需要继续检查 Gatekeeper 的信任与公证信息。

## 已知问题与风险

- **CGO 依赖**：webview_go 需要 CGO + macOS Xcode Command Line Tools（链接 WebKit）；Windows 需 WebView2 Runtime。
- **Go proxy**：本机 `GOPROXY` 指向 `http://goproxy.bilibili.co` 已失效，拉取新依赖需改用 `https://proxy.golang.org,direct` 或 `https://goproxy.cn,direct`。
- **webview_go 上游**：维护不活跃；当前用 upstream module，若 macOS 新版本出现兼容问题，改 vendor Ollama 的 fork。
- **编辑快捷键失效**：上游 webview 的 Cocoa 后端只设置了 app 激活策略，没有创建 `NSMenu`/`setMainMenu:`，导致 `Cmd+A`（全选）、`Cmd+C/V/X`（复制/粘贴/剪切）等编辑快捷键在 WKWebView 里不生效。当前对搜索框的 `Cmd/Ctrl+A` 在 `SearchBar` 里用 `onKeyDown` + `select()` 做了前端兜底；复制粘贴等其余快捷键留待 vendor Ollama fork（其已内置 Edit 菜单）时一并修复。
- **前端未构建**：`go build ./desktop` 前若未执行 `npm run build`，只会内嵌占位 `placeholder.txt`，运行时提示先构建前端。Vite 每次构建前清空 `dist/`，避免旧的 hash 产物残留；占位文件在配置加载时读取，再随构建重新输出，保持入库文件不变。
- **macOS 安装包未做 Developer ID 签名/公证**：当前仅对完整 `.app` 做 ad-hoc 签名与完整性校验，不保证通过 Gatekeeper；正式分发需补 Developer ID 签名与 Apple 公证。
- **仅 Apple Silicon（arm64）**：release 当前只在 `macos-latest`（arm64）构建，未出 Intel（x86_64）与通用二进制包。`macos-13` 已退役，Intel 需 `macos-15-intel` 等 large runner，或交叉编译 + `lipo` 合通用包。

## 分阶段计划

- **Phase 1（已完成）**：MVP —— 搜索 + 实时行情，跑通「WebView + 本地 API + 内嵌前端」全链路。
- **Phase 2（进行中）**：研究工作台、公司信息/分红、财报、IPO 上市信息、公告、自选股与偏好持久化已接入；后续补充申购排期、多组合与 SQLite 迁移等。
- **Phase 3**：系统托盘、自动更新、Windows 支持、`.exe` 打包、Intel/通用二进制。macOS `.app`/`.dmg` 打包与 release CI 已完成。
