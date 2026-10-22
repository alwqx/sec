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
- **持久化**：MVP 不引入；Phase 2 用 SQLite 存自选股/设置。
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

## 前端

- 技术：React 18 + TypeScript + Vite 5，手写 CSS（暗色主题，红涨绿跌）。
- 交互：搜索框 → 结果列表 → 点击某只证券 → 行情卡片（每 5 秒轮询刷新，实现「实时行情」观感）。
- 通过 Vite `server.proxy` 把 `/api` 代理到 dev 模式的 Go server（`127.0.0.1:3001`）。

## 构建与开发

```bash
# 前端（开发热更新 / 生产构建）
cd desktop/ui && npm install && npm run dev        # 开发
cd desktop/ui && npm run build                     # 生产（生成 dist/）

# 桌面端
go run ./desktop -dev                        # 联调：WebView 加载 http://localhost:5173，server 固定 3001
go build -o sec-desktop ./desktop            # 正式构建（需先 npm run build 生成 dist）
```

### 发布（macOS）

`.github/workflows/release.yml` 的 `build-macos-desktop` job 在 `macos-latest`（Apple Silicon，arm64）上：构建前端 → `go build ./desktop` → 组装 `sec.app`（元数据见 `desktop/Info.plist`）→ `hdiutil` 打包成 `sec-desktop-darwin-arm64.dmg` 并上传到 Release。安装包未签名/未公证，首次打开需右键「打开」绕过 Gatekeeper。

## 已知问题与风险

- **CGO 依赖**：webview_go 需要 CGO + macOS Xcode Command Line Tools（链接 WebKit）；Windows 需 WebView2 Runtime。
- **Go proxy**：本机 `GOPROXY` 指向 `http://goproxy.bilibili.co` 已失效，拉取新依赖需改用 `https://proxy.golang.org,direct` 或 `https://goproxy.cn,direct`。
- **webview_go 上游**：维护不活跃；当前用 upstream module，若 macOS 新版本出现兼容问题，改 vendor Ollama 的 fork。
- **编辑快捷键失效**：上游 webview 的 Cocoa 后端只设置了 app 激活策略，没有创建 `NSMenu`/`setMainMenu:`，导致 `Cmd+A`（全选）、`Cmd+C/V/X`（复制/粘贴/剪切）等编辑快捷键在 WKWebView 里不生效。当前对搜索框的 `Cmd/Ctrl+A` 在 `SearchBar` 里用 `onKeyDown` + `select()` 做了前端兜底；复制粘贴等其余快捷键留待 vendor Ollama fork（其已内置 Edit 菜单）时一并修复。
- **前端未构建**：`go build ./desktop` 前若未执行 `npm run build`，只会内嵌占位 `placeholder.txt`，运行时提示先构建前端。Vite 每次构建前清空 `dist/`，避免旧的 hash 产物残留；占位文件在配置加载时读取，再随构建重新输出，保持入库文件不变。
- **macOS 安装包未签名/未公证**：release 打出的 `.dmg` 未做 Developer ID 签名与 notarization，用户首次打开会被 Gatekeeper 拦截，需右键「打开」；正式分发需补签名 + 公证。
- **仅 Apple Silicon（arm64）**：release 当前只在 `macos-latest`（arm64）构建，未出 Intel（x86_64）与通用二进制包。`macos-13` 已退役，Intel 需 `macos-15-intel` 等 large runner，或交叉编译 + `lipo` 合通用包。

## 分阶段计划

- **Phase 1（当前）**：MVP —— 搜索 + 实时行情，跑通「WebView + 本地 API + 内嵌前端」全链路。
- **Phase 2**：公司信息/分红、财报、IPO 日历、公告、自选股（SQLite 持久化）。
- **Phase 3**：系统托盘、自动更新、Windows 支持、`.exe` 打包、Intel/通用二进制。macOS `.app`/`.dmg` 打包与 release CI 已完成。
