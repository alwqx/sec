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

个股「财报」页分为「财务报表」与「定期报告」。财务报表可按当前报表类型和报告期重新查询，导出 CSV 或 JSON，最多 50 期，保留全部原始字段和数值单位（不按页面的亿元缩放）。CSV 使用 UTF-8 BOM，JSON 包含证券信息、报告日期及原始字段。

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
- dev 模式跳过鉴权并打开 CORS，便于前端连 Vite dev server。

## 研究工作台

### 页面与导航

- 「自选与行情」证券列表上方提供证券搜索，支持名称、代码与拼音；搜索保留当前研究证券，查询结果显示在列表中，其他页面不显示证券搜索框。
- 导航图标使用 `lucide-react` 静态导入的 SVG：自选与行情 ChartCandlestick、IPO FilePlusCorner、公告中心 Newspaper、黄金行情 Coins、美债收益率 Landmark、设置 Settings。统一 20px、1.75px 描边，使用 currentColor 跟随主题和选中状态；保留文字标签，装饰图标不进入无障碍名称。
- 左侧全局导航：自选与行情、IPO、公告中心、黄金行情、美债收益率、设置。
- 自选与行情：中间切换「我的自选 / 搜索结果」，右侧围绕选中证券展示「概览 / 公司 / 财报 / 分红 / 公告」。切换全局页面保留当前证券与个股栏目。
- 概览：实时行情、可选择日期范围与复权方式的日 K 线蜡烛图（开高低收）、成交量副图与所选交易日的成交额。仅概览页按偏好轮询行情，其他页面不轮询；历史走势默认近 3 个月、不复权，支持近 1／3／6 个月、近 1 年及自定义日期（1990 年至上海今日，单次最多 5 年），支持不复权／前复权／后复权；查询后显示已生效条件，复权不改变实时行情报价。历史走势进入概览或提交条件时加载。切换证券与页面取消旧请求。
- 公司、财报、分红、个股公告：暂仅支持沪深北 A 股；财报支持资产负债表、利润表、现金流量表与报告期筛选，金额单位亿元，每股收益元/股。历史走势图支持沪深证券与港股，其余市场保留实时行情。
- IPO：分为「新股上市」与「招股说明书」两个栏目。「新股上市」用表格展示东方财富上市信息，支持上市日期起止筛选（可只填一边、包含边界）、重置、筛选后分页和进入个股研究；查询或重置回到第一页，刷新与翻页使用已提交条件。有日期条件时排除日期待定记录，先过滤完整源列表再分页，计数为筛选后总数；无条件时保留原源分页及日期待定记录。CLI 与 Desktop 共用 `eastmoney.ListIPOFiltered`，不完整源响应返回错误，避免显示不完整的筛选结果；合并原有上市排期与新股列表，避免同一数据重复展示。「招股说明书」栏目，支持六位沪深北证券代码（可带 SH/SZ/BJ 前缀）、可选公告起止日期及分页；新股列表可点击「招股书」带入代码。复用 CLI 的首次公开发行及上市分类，包含招股说明书、招股意向书和发行公告，仅展示有效 PDF。支持原文查看、本页全选、单份／多选顺序下载，显示进度和各文件失败信息；取消保留已完成文件，重新查询或离开栏目取消未完成请求。下载复用设置目录、同名编号、临时文件清理及巨潮 HTTPS PDF 校验（每份最大 128 MB）。
- 公告中心：巨潮资讯最新披露 / 当前 A 股证券，可按标题关键字与公告类别（全部／年报／中报／一季报／三季报）组合查询并分页，切换类别回到第一页并保留已提交关键字；同样适用于个股公告栏目。仅展示有效公告；标题按纯文本呈现，原文链接只允许巨潮资讯域名。桌面通过 Go 绑定在系统浏览器打开原文；开发浏览器直接打开新标签，并提供复制链接。
- 设置：行情刷新间隔 5/15/30/60 秒，深色、浅色和跟随系统外观。窄窗口将导航横排、证券列表与详情上下排列；个股栏目支持方向键、Home、End 切换。

### 黄金行情

「黄金行情」复用 CLI `metal` / `metal-history` 的 `provider/metal.QueryAu999`，目前提供上海黄金交易所 Au99.99。顶部展示截至上海时区今日的最近可用日行情，包含交易日期、收盘、开盘、最高、最低、昨收、涨跌额与涨跌幅，价格单位元/克；它是日行情而非盘中报价，休市时保留最近交易日。

历史区间默认从上海时区今日向前 30 天，提供近 1 个月 / 3 个月 / 1 年快捷区间与自定义起止日期；以日 K 线蜡烛图和最新在前的明细表呈现；蜡烛实体表示开盘至收盘、影线表示最高至最低，按收盘相对开盘红涨绿跌、平盘灰色，支持悬停/点击查看开高低收与方向键选择交易日，长区间横向滚动且左侧价格轴保持固定可见，明细每页 30 条。单次接口请求复用上游全量日行情，同时返回最新记录与过滤后的历史，避免为最新、历史重复抓取。修改历史范围不会把顶部最新行情替换成区间末日，首个历史交易日的昨收也保留区间外基准。

页面提供手动刷新、请求取消、失败重试、日期错误提示与空数据状态；不按股票行情间隔轮询。没有有效昨收时，昨收、涨跌额和涨跌幅返回 `null` 并显示「—」，不将缺失值解释为零涨跌。黄金与个股历史共用 `CandlestickChart`，均支持固定价格轴、悬停/点击与键盘选择，以及长区间横向滚动；证券历史保持不复权，沪深证券价格及成交额单位为元、港股为港元；成交量按 provider 归一化结果以股/份显示（沪深已从手转换，不再重复乘 100），成交量副图与价格图共享横轴、固定纵轴和交易日选择。黄金数据源不提供量额，因此黄金页只绘制价格 K 线。

### 公司核心指标

个股概览与公司页均展示行业、市盈率 PE（TTM）、市净率 PB、总市值和流通市值。目前沿用公司资料栏目的沪深北 A 股范围；基金、港股和美股不请求该指标接口。

`GET /api/v1/metrics?code=SH600036` 只请求新浪行情与扩展行情，不依赖公司资料 HTML。通过 `provider/sina.QueryCoreMetrics` 加载，与 CLI `info` 共用计算函数：PB = 当前价 / 每股净资产，PE（TTM）沿用 CLI 的股本及利润字段口径，A 股总/流通市值 = 当前价 × 总/流通股本 × 10000。返回行业、倍数、市值（人民币元）及行情快照日期/时间；界面将市值格式化为万元、亿元、万亿元。亏损或缺少利润数据时 PE 显示「—」，无法计算、零占位及非有限数值也显示「—」，不误报为 0 倍或 0 元；可计算的负 PB 保留。

概览中核心指标每次请求完成后等待 60 秒刷新，可手动刷新；公司页按进入时加载并提供手动刷新。核心指标、公司资料和实时行情分别加载、分别重试，一项失败不会阻塞其他内容。自动刷新失败保留并标明上次成功快照；切换证券或页面时取消请求。指标快照时间与实时行情刷新周期分别标注。

### 美债收益率

「美债收益率」复用 CLI `bond` / `bond-history` 的美国财政部年度 XML 数据，展示 1/3/6 个月、1/2/3/5/7/10/20/30 年共 11 个期限。最新交易日独立于历史查询区间；日期使用 `America/New_York` 时区，日收益率单位为百分比，10 年期日变动为 bp（1 bp = 0.01 个百分点）。红绿仅表示收益率上升/下降，不表示债券价格涨跌。

页面提供最新期限明细、可切换日期的收益率曲线、可切换期限的历史折线与每页 30 条明细。收益率曲线横轴按期限等距排列，历史横轴按交易日顺序排列；选择历史期限后，修改日期范围仍保留该期限。默认最近 30 天，支持近 1 个月 / 3 个月 / 1 年快捷区间与自定义日期；允许 1990 年至美东今日，单次历史范围最多 5 年，避免一次抓取过多年度数据。提供手动刷新、请求取消、失败重试与空区间状态，不进行盘中轮询。

`GET /api/v1/bond?begin=2026-01-01&end=2026-01-31` 返回 `tenors`、`latest`、`results` 及实际起止日期。接口只抓取历史区间、前值缓冲和最新行情所需年份的并集，每年一次；补读开始日期之前 35 天所在年份，保留跨年首日的日变动基准。任一年查询失败返回 502，不将不完整历史显示为查询成功；不连续年份之间也不会计算虚假的日变动。

provider 在保持原有数值字段的同时，通过 `MissingTenors` 记录上游省略、空值或 `m:null` 的期限，桌面以 `null` /「—」呈现并在折线中断开；真实 0% 仍是有效数值。没有有效前一交易日基准时，前值与 bp 变动也显示「—」。

### 自选与设置持久化

自选通过「加入自选 / 已加入自选」按钮增删，CLI 与 Desktop 共享 `watchlist` 包和 `~/.sec/watchlist.json`。共享记录包含代码、带市场前缀标识、名称、证券类型、交易所及添加时间；按标准标识去重，最多新增到 500 只。旧数据超过上限时不截断，仍允许删除。跨进程操作使用 `gofrs/flock` 文件锁，读改写在同一锁内完成，以同目录临时文件、同步写入和原子替换发布。锁文件保留，进程退出由操作系统释放锁。

首次访问共享存储时合并旧 CLI 数组与 Desktop 自选，原文件生成同目录 `*.pre-shared-*.bak` 备份；版本化共享文档作为一次性迁移标记，随后删除 workspace 的旧 watchlist 字段。中断重试不会把已删除证券重新导入。迁移不依赖网络；旧 CLI 缺少类型时按代码补齐常见基金类型，其余默认股票，重复记录优先保留 Desktop 的完整证券信息及 CLI 原有添加时间。损坏文件返回错误并保留原文。旧版 CLI 不能读取新格式，两端应同步使用新版。

设置仍保存到 `~/.sec/desktop/workspace.json`，仅持久化 settings。保存设置即使携带过期 watchlist 快照也不会改写共享自选；增删使用独立接口，只提交对应证券。首次保存设置前先完成旧列表迁移，设置文件与迁移使用同一工作台文件锁。Desktop 返回「自选与行情」、重新获得窗口焦点或点击「刷新自选」时同步列表，后台响应不会覆盖本窗口正在进行的增删。当前研究证券、筛选条件与菜单仅在本次运行中保留。

### 新增 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
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
| GET | `/api/v1/financials?code=SH600036&report=balance&period=annual` | 财报；report 为 balance/income/cashflow，period 可省略或 annual/halfyear/q1/q3 |
| GET | `/api/v1/bond?begin=2026-01-01&end=2026-01-31` | 美国国债最新各期限收益率与区间历史；起止日期 YYYY-MM-DD，默认美东今日向前 30 天，单次最多 5 年 |
| GET | `/api/v1/metal?begin=2026-01-01&end=2026-01-31` | Au99.99 最新日行情与区间历史；日期格式 YYYY-MM-DD，省略 end 时默认上海时区今日，省略 begin 时默认 end 向前 30 天 |
| GET | `/api/v1/history?code=SH600036&begin=2024-01-01&end=2024-12-31&fq=qfq` | 沪深/港股历史开高低收与量额（date/open/close/high/low/volume/turnover）；日期为 YYYY-MM-DD，默认近 3 个月，fq 为 bfq/qfq/hfq（默认 bfq），单次最多 5 年 |
| GET | `/api/v1/ipo/prospectuses?code=300750&begin=2018-01-01&end=2018-12-31&page=1` | 招股及 IPO 相关有效 PDF，日期可省略，返回 code/name/results/page/has_more |
| POST | `/api/v1/ipo/prospectuses/download` | JSON 参数 code/url，保存单份 PDF 并返回本地路径；多选由前端逐份调用 |
| GET | `/api/v1/ipo?page=1&since=2024-01-01&until=2024-12-31` | 每页 30 条新股，since/until 为可选上市日期边界（YYYY-MM-DD），返回筛选后的 total 与 has_more |
| GET | `/api/v1/announcements?page=1&code=SH600036&key=年度报告&type=annual` | code 可省略以查询最新披露，支持分页与标题关键字；type 可省略查询全部，或为 annual/halfyear/q1/q3，对应巨潮公告分类 |

新增接口统一沿用 token cookie 鉴权。上游失败返回 502；公司资料为空返回 404；参数无效返回 400；列表为空返回 `{"results":[]}`（附带各接口其他元数据）。写入设置额外限制请求体大小、内容类型、同源与有效证券/偏好字段。

## 前端

React 18 + TypeScript + Vite 5，手写 CSS。`desktop/ui/src/components/` 按研究栏目与全局页面拆分，`hooks/useResource.ts` 统一处理数据加载、错误重试与取消。Vite `server.proxy` 将 `/api` 代理到开发模式 Go server（`127.0.0.1:3001`），不依赖浏览器跨域直连。

## 构建与开发

```bash
# 前端（开发热更新 / 生产构建）
cd desktop/ui && npm install && npm run dev        # 开发
cd desktop/ui && npm run build                     # 生产（生成 dist/）

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
