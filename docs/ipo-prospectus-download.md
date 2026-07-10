# IPO 招股书下载方案设计

> 目标：为 `sec` CLI 增加**下载** IPO 招股说明书 PDF 的能力。
> 状态：方案阶段（2026-07-09）
> 关联：[research-ipo.md](./research-ipo.md) — IPO 功能调研文档

---

## 一、现状分析

### 1.1 已有能力

| 能力          | 命令/函数                                       | 说明                                                      |
| ------------- | ----------------------------------------------- | --------------------------------------------------------- |
| 招股书列表    | `sec ipo prospectus <code>`                     | 列出指定股票的 IPO 相关公告（标题、日期、大小、PDF 链接） |
| IPO 公告查询  | `cninfo.QueryIPOs(ctx, stockParam, size)`       | 从巨潮资讯查询 IPO 公告列表，含 PDF URL                   |
| PDF 下载      | `cninfo.DownloadPDF(ctx, adjunctURL, destPath)` | 从巨潮 CDN 下载单个 PDF 到本地路径                        |
| 年报 PDF 下载 | `sec bsd <code> -y 2024`                        | **已有参考实现**：下载年报 PDF 的完整命令                 |

### 1.2 现有代码路径

```
cmd/ipo/ipo.go
  └── runProspectus()            → 查询 + 列表渲染（无下载）
  └── printProspectus()          → tablewriter 表格输出

provider/cninfo/cninfo.go
  └── QueryIPOs()                → 招股书公告查询
  └── DownloadPDF()              → 单文件 PDF 下载（已可用）
  └── resolvePDFURL()            → PDF 直链解析

cmd/balancesheet/download.go     → sec bsd 参考实现
  └── BalanceSheetDownloadHandler()
```

### 1.3 差距

- `sec ipo prospectus` **只能看不能下**：用户看到了 PDF 链接，但没有下载功能
- `cninfo.DownloadPDF()` 已经就绪，只需上层命令调用
- 缺少：交互选择、批量下载、文件命名策略、断点续传/跳过已有

---

## 二、需求梳理

### 2.1 核心场景

| 场景           | 用户意图                                             | 优先级 |
| -------------- | ---------------------------------------------------- | ------ |
| 下载最新招股书 | `sec ipo download 300750` 下载宁德时代最新一份招股书 | P0     |
| 下载全部招股书 | `sec ipo download 300750 --all` 下载该股所有招股书   | P0     |
| 交互选择下载   | 列表展示后，用户输入序号选择要下载的文件             | P1     |
| 批量下载       | 一次下载多只股票的招股书                             | P2     |
| 港股招股书     | 通过 HKEX 下载港股招股书                             | P2     |
| 美股招股书     | 通过 SEC EDGAR 下载 S-1/F-1                          | P3     |

### 2.2 功能需求

1. **下载招股书 PDF**：从巨潮资讯下载 A 股 IPO 招股说明书
2. **灵活选择**：支持下载最新一份、全部、或交互选择
3. **输出目录**：支持 `-o` 指定保存目录
4. **文件命名**：规范的命名格式，包含代码、名称、日期
5. **下载反馈**：显示下载进度、文件大小、保存路径
6. **跳过已有**：文件已存在时自动跳过（或 --force 覆盖）

---

## 三、方案对比

### 方案 A：在现有 `prospectus` 命令增加 `--download` 标志

```
sec ipo prospectus 300750 --download          # 下载最新的
sec ipo prospectus 300750 --download --all    # 下载全部
sec ipo prospectus 300750 --download -o ./pdfs
```

**优点**：

- 改动最小，复用现有查询/展示逻辑
- 用户只需记一个命令

**缺点**：

- 命令职责混杂（查询+下载）
- `prospectus` 语义偏向"查看"，加上 `--download` 语义不够清晰
- 无法支持交互选择（列表已经渲染完了才决定要下载）

### 方案 B：新增 `sec ipo download` 子命令（推荐）

```
sec ipo download 300750                  # 下载最新一份
sec ipo download 300750 --all            # 下载全部
sec ipo download 300750 --index 1,3      # 下载指定序号
sec ipo download 300750 -o ./pdfs        # 指定目录
sec ipo download 300750 --dry-run        # 只看不下载
```

**优点**：

- 职责清晰：`prospectus` 看、`download` 下
- 符合现有模式：`sec balance-sheet` 看报表、`sec bsd` 下年报
- 交互式选择自然（先展示列表，再让用户选）
- 可以设置别名：`sec ipo dl`

**缺点**：

- 多一个子命令（但项目已有模式支持）

### 方案 C：新增顶层命令 `sec ipod`（类似 `sec bsd`）

```
sec ipod 300750                  # 类似 sec bsd
sec ipod 300750 --all
```

**优点**：

- 与 `sec bsd` 完全对称

**缺点**：

- 顶层命令越来越多，不利于组织
- `ipo` 已有命令组，放在组下更自然

### 推荐：方案 B

理由：

1. 与现有 `sec ipo` 命令组内聚
2. 职责单一，语义清晰
3. 便于后续扩展（港股、美股招股书下载可加 `--market` 参数）

---

## 四、详细设计（方案 B）

### 4.1 命令签名

```
sec ipo download <code> [flags]

Aliases: dl, d

Flags:
  -n, --size int         查询公告数量 (default 30)
  -o, --output-dir string   PDF 保存目录 (default "./")
  -a, --all               下载查询到的所有招股书
  -i, --index string      下载指定序号的公告 （如 "1,3,5")
      --force             覆盖已存在的文件
      --dry-run           仅列出，不下载
      --since string      起始日期 (YYYY-MM-DD)
      --until string      截止日期 (YYYY-MM-DD)
  -D, --debug             启用调试日志
```

### 4.2 交互流程

```
$ sec ipo download 300750

宁德时代 (300750) IPO 相关公告 （来源：巨潮资讯）:

  #  公告日期      公告标题                                    大小
  1  2018-05-22   首次公开发行股票并在创业板上市招股说明书       12.5 MB
  2  2018-05-22   首次公开发行股票并在创业板上市公告书           3.2 MB
  3  2018-06-08   首次公开发行股票并在创业板上市之上市公告书     2.8 MB

请选择要下载的编号 (all=全部，q=退出，默认=1): _
```

- 默认行为（无 `--all` / `--index` / `--dry-run`）：显示列表并交互选择
- `--all`：跳过交互，直接下载全部
- `--index 1,3`：跳过交互，下载指定编号
- `--dry-run`：只显示列表不下载（相当于现有 `prospectus` 但加上编号）

### 4.3 文件命名策略

```
格式：{code}_{name}_{title_short}_{date}.pdf

示例：
  300750_宁德时代_招股说明书_20180522.pdf
  300750_宁德时代_上市公告书_20180608.pdf
  600036_招商银行_招股意向书_20020322.pdf
```

命名规则：

- `code`：6 位股票代码
- `name`：证券简称
- `title_short`：从公告标题提取关键词（招股说明书 / 招股意向书 / 上市公告书），长度 ≤20 字符
- `date`：公告日期 YYYYMMDD

### 4.4 下载输出

```
$ sec ipo download 300750

宁德时代 (300750) IPO 相关公告 （来源：巨潮资讯）:

  #  公告日期      公告标题                                    大小
  1  2018-05-22   首次公开发行股票并在创业板上市招股说明书       12.5 MB
  2  2018-05-22   首次公开发行股票并在创业板上市公告书           3.2 MB

请选择要下载的编号 (all=全部，q=退出，默认=1): 1

下载中。..
  [1/1] 300750_宁德时代_招股说明书_20180522.pdf ... 12.5 MB ✓

已保存至：./300750_宁德时代_招股说明书_20180522.pdf
```

### 4.5 跳过已有文件

```
$ sec ipo download 300750 --all

  [1/2] 300750_宁德时代_招股说明书_20180522.pdf ... 已存在，跳过
  [2/2] 300750_宁德时代_上市公告书_20180608.pdf ... 3.2 MB ✓

下载完成：1 新下载，1 跳过，0 失败
```

使用 `--force` 覆盖已存在的文件。

---

## 五、代码结构设计

### 5.1 文件变更

```
新增文件：
  cmd/ipo/download.go          # download 子命令 + runDownload handler

修改文件：
  cmd/ipo/ipo.go               # NewIPOCLI() 中注册 download 子命令
  provider/cninfo/cninfo.go     # 无需修改（已满足需求）
  CHANGELOG.md                  # 记录新功能

可选优化：
  cmd/ipo/ipo.go               # 提取公共函数 (resolveStockCode 等） 供 prospectus 和 download 共用
```

### 5.2 核心数据结构

```go
// downloadConfig holds the parsed download options.
type downloadConfig struct {
    code      string   // 股票代码（用户输入）
    size      int      // 查询条数
    outputDir string   // 输出目录
    all       bool     // 下载全部
    index     []int    // 指定序号（1-based）
    force     bool     // 覆盖已有
    dryRun    bool     // 仅预览
    since     string   // 起始日期
    until     string   // 截止日期
}
```

### 5.3 核心函数签名

```go
// cmd/ipo/download.go

func newDownloadCmd() *cobra.Command
func runDownload(cmd *cobra.Command, args []string) error

// 交互选择
func promptSelection(out io.Writer, in io.Reader, max int) ([]int, error)

// 文件名生成
func buildFilename(code, name string, a *cninfo.Announcement) string

// 标题关键词提取
func extractShortTitle(title string) string

// 公告过滤（复用现有 filterByDate）
```

### 5.4 公共函数提取（可选优化）

`runProspectus` 和 `runDownload` 共享的步骤可提取：

```go
// resolveStock 将用户输入解析为标准 A 股代码 + orgId + 名称
func resolveStock(ctx context.Context, input string) (code, orgID, name string, err error)
```

提取后可减少 `download.go` 与 `prospectus` 的重复代码。

---

## 六、实现计划

### Phase 1: MVP（P0）

| 步骤 | 内容                                            | 预估   |
| ---- | ----------------------------------------------- | ------ |
| 1    | 提取 `resolveStock()` 公共函数                  | 10 min |
| 2    | 在 `cmd/ipo/ipo.go` 注册 `download` 子命令      | 5 min  |
| 3    | 实现 `cmd/ipo/download.go`：查询 + 默认交互下载 | 30 min |
| 4    | 实现 `--all` 和 `--index` 模式                  | 15 min |
| 5    | 实现 `--output-dir` / `--force` / `--dry-run`   | 15 min |
| 6    | `go fmt` + 编译测试 + 手动验证                  | 15 min |
| 7    | 更新 CHANGELOG.md                               | 5 min  |

**Phase 1 总预估：~1.5 小时**

### Phase 2: 增强（P1）

| 步骤 | 内容                                        |
| ---- | ------------------------------------------- |
| 1    | 使用 `progressbar` 库显示下载进度条         |
| 2    | 支持 `--concurrency` 并发下载               |
| 3    | 交互式 TUI（使用 bubbletea 或简单终端交互） |

### Phase 3: 港股 + 美股（P2-P3）

| 步骤 | 内容                                             |
| ---- | ------------------------------------------------ |
| 1    | `provider/hkex/prospectus.go` — HKEX 招股书查询  |
| 2    | `provider/sec/edgar.go` — SEC EDGAR S-1/F-1 查询 |
| 3    | `--market hk` / `--market us` 参数支持           |

---

## 七、风险与注意事项

| 风险                                | 缓解措施                                                 |
| ----------------------------------- | -------------------------------------------------------- | --------------------------- |
| 巨潮 PDF URL 带权限/token 过期      | 下载前重新解析 URL（`resolvePDFURL` 实时计算）           |
| PDF 文件较大（招股书可达 30-50 MB） | `cninfo.DownloadPDF` 已有 10min 超时；下载时显示文件大小 |
| 巨潮请求频率限制                    | `download --all` 时每个请求间隔 2-3 秒                   |
| 文件名过长（标题超长）              | `extractShortTitle` 限制 ≤20 字符                        |
| 文件名含非法字符                    | 过滤 `/\:\*?"<>                                          | ` 等 Windows/macOS 非法字符 |
| 网络中断导致半截文件                | 先下载到临时文件（`.tmp` 后缀），完成后再 rename         |

---

## 八、完整命令示例

```bash
# 交互下载（默认行为）
sec ipo download 300750

# 下载最新一份（默认=1，无需交互）
sec ipo download 300750 --index 1

# 下载指定序号的
sec ipo download 300750 -i 1,3,5

# 下载全部
sec ipo download 300750 --all

# 指定输出目录
sec ipo download 300750 -o ~/Documents/招股书/

# 覆盖已有
sec ipo download 300750 --all --force

# 预览模式（看看有什么可下载）
sec ipo download 300750 --dry-run

# 日期范围过滤后下载
sec ipo download 300750 --since 2018-01-01 --until 2018-12-31 --all

# 别名
sec ipo dl 300750
sec ipo d 300750
```

---

## 九、与其他命令的关系

```
sec ipo list            → 查看 IPO 上市列表（来自东方财富）
sec ipo calendar        → 查看 IPO 排期/日历（来自巨潮资讯）
sec ipo prospectus      → 查看某股的招股书列表（来自巨潮资讯）
sec ipo download  ←NEW → 下载某股的招股书 PDF（来自巨潮资讯）
sec bsd                 → 下载某股的年报 PDF（对比参考）
```

---

## 十、总结

**推荐方案 B**：在 `sec ipo` 命令组下新增 `download` 子命令。

核心思路：

1. 复用现有 `cninfo.QueryIPOs()` + `cninfo.DownloadPDF()` 能力
2. 参考 `sec bsd` 的下载命令模式
3. 支持交互选择 + 命令行参数两种使用方式
4. Phase 1 聚焦 A 股 MVP，Phase 2-3 扩展港股/美股

实施成本低（~1.5h），代码改动集中在 `cmd/ipo/` 包内，不涉及 provider 层变更。
