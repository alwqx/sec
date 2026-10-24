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
- **持久化**：工作台首版采用 JSON 原子替换，将自选股/设置保存到 `~/.sec/desktop/workspace.json`；后续需要多组合与历史查询时再迁移 SQLite。
- **平台**：macOS 优先；Windows 打包放 Phase 3。

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

- 顶部全局证券搜索，支持名称、代码与拼音；搜索保留当前研究证券，查询结果显示在中间列表。
- 左侧全局导航：自选与行情、IPO 日历、公告中心、设置。
- 自选与行情：中间切换「我的自选 / 搜索结果」，右侧围绕选中证券展示「概览 / 公司 / 财报 / 分红 / 公告」。切换全局页面保留当前证券与个股栏目。
- 概览：实时行情、近 60 个交易日不复权收盘价走势。仅概览页按偏好轮询行情，其他页面不轮询；历史走势按进入概览时加载。切换证券与页面取消旧请求。
- 公司、财报、分红、个股公告：暂仅支持沪深北 A 股；财报支持资产负债表、利润表、现金流量表与报告期筛选，金额单位亿元，每股收益元/股。历史走势图支持沪深证券与港股，其余市场保留实时行情。
- IPO 日历：东方财富新股列表，按当前页上市日期分组或表格展示，支持分页和进入个股研究。上游列表没有申购日期，界面不推算或填充申购排期。
- 公告中心：巨潮资讯最新披露 / 当前 A 股证券，可按关键字查询并分页。仅展示有效公告；标题按纯文本呈现，原文链接只允许巨潮资讯域名。桌面通过 Go 绑定在系统浏览器打开原文；开发浏览器直接打开新标签，并提供复制链接。
- 设置：行情刷新间隔 5/15/30/60 秒，深色、浅色和跟随系统外观。窄窗口将导航横排、证券列表与详情上下排列；个股栏目支持方向键、Home、End 切换。

### 自选与设置持久化

自选通过「加入自选 / 已加入自选」按钮增删，最多保存 500 只。设置点击保存后生效。Go 服务将完整状态写入 `~/.sec/desktop/workspace.json`，使用同目录临时文件、同步写入与原子替换；数据损坏时返回错误并阻止界面覆盖，提供重新加载入口。

文件不依赖 WebView 的随机端口，因此重启后能恢复自选和偏好。与 CLI 的 `~/.sec/watchlist.json` 独立，不自动导入或覆盖 CLI 自选。首版采用 JSON，以便直接复用现有工程依赖；SQLite、多组合与跨进程同步留待后续扩展。当前研究证券、筛选条件与菜单仅在本次运行中保留。

### 新增 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/workspace` | 本地自选与设置；无文件时返回默认值 |
| PUT | `/api/v1/workspace` | 保存完整状态，要求 JSON、有效参数及生产同源请求 |
| GET | `/api/v1/company?code=SH600036` | 公司资料 |
| GET | `/api/v1/dividends?code=SH600036` | 分红记录，按每 10 股展示税前派息/送转 |
| GET | `/api/v1/financials?code=SH600036&report=balance&period=annual` | 财报；report 为 balance/income/cashflow，period 可省略或 annual/halfyear/q1/q3 |
| GET | `/api/v1/history?code=SH600036` | 最近 60 个交易日历史收盘价，沪深/港股 |
| GET | `/api/v1/ipo?page=1` | 每页 30 条新股，返回 total 与 has_more |
| GET | `/api/v1/announcements?page=1&code=SH600036&key=年度报告` | code 可省略以查询最新披露，支持分页与标题关键字 |

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

`.github/workflows/release.yml` 的 `build-macos-desktop` job 在 `macos-latest`（Apple Silicon，arm64）上：构建前端 → `go build ./desktop` → 组装 `sec.app`（元数据见 `desktop/Info.plist`）→ 对完整应用执行 ad-hoc 签名与严格校验 → `hdiutil` 打包成 `sec-desktop-darwin-arm64.dmg` → 校验并挂载镜像验证内部应用签名 → 上传到 Release。所有资源和元数据必须在签名前写入，避免裸二进制的链接器签名与应用包不匹配。

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
- **Phase 2（进行中）**：研究工作台、公司信息/分红、财报、IPO 上市排期、公告、自选股与偏好持久化已接入；后续补充申购排期、多组合与 SQLite 迁移等。
- **Phase 3**：系统托盘、自动更新、Windows 支持、`.exe` 打包、Intel/通用二进制。macOS `.app`/`.dmg` 打包与 release CI 已完成。
