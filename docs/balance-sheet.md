# sec balance-sheet — 财务报表查询与下载

## 调研概述

为 `sec` 增加财务报表查询与下载功能：输入证券代码，列出该证券的全部财务报表，并支持下载报表数据到本地文件。

## 数据源选择

### 主数据源：东方财富 datacenter-web API

| 属性           | 值                                                     |
| -------------- | ------------------------------------------------------ |
| Base URL       | `https://datacenter-web.eastmoney.com/api/data/v1/get` |
| 认证           | 无需认证（免费公开）                                   |
| 格式           | JSONP（需去壳）                                        |
| 可靠性         | 高（AKShare、InvesTool 等开源项目均在使用）            |
| 与现有项目兼容 | 项目已使用东方财富 `push2his` 接口获取 K 线            |

### 备选数据源：新浪财经

| 属性 | 值                                                                                      |
| ---- | --------------------------------------------------------------------------------------- |
| URL  | `https://vip.stock.finance.sina.com.cn/corp/go.php/vFD_BalanceSheet/stockid/{CODE}/...` |
| 格式 | HTML（需 JS 渲染）或 `quotes.sina.cn` JSON API                                          |
| 问题 | HTML 页面需要 headless browser；JSON API 字段名不规则                                   |

**结论：优先使用东方财富 datacenter-web API。**

---

## API 详解

### 请求格式

```
GET https://datacenter-web.eastmoney.com/api/data/v1/get?{params}
```

### 参数

| 参数          | 说明                     | 示例值                                           |
| ------------- | ------------------------ | ------------------------------------------------ |
| `callback`    | JSONP 回调名（必需）     | `jQuery112306144579{随机}_{时间戳}`              |
| `reportName`  | 报表类型代码             | 见下方                                           |
| `columns`     | 返回字段，`ALL` 表示全部 | `ALL`                                            |
| `filter`      | 过滤条件                 | `(SECURITY_CODE="000001")(DATE_TYPE_CODE="001")` |
| `sortColumns` | 排序字段                 | `REPORT_DATE`                                    |
| `sortTypes`   | 排序方向                 | `-1`（降序）/ `1`（升序）                        |
| `pageNumber`  | 页码                     | `1`                                              |
| `pageSize`    | 每页条数                 | `50`                                             |

### 三大报表的 reportName

| 报表                             | reportName             |
| -------------------------------- | ---------------------- |
| 资产负债表 (Balance Sheet)       | `RPT_DMSK_FN_BALANCE`  |
| 利润表 (Income Statement)        | `RPT_DMSK_FN_INCOME`   |
| 现金流量表 (Cash Flow Statement) | `RPT_DMSK_FN_CASHFLOW` |

### DATE_TYPE_CODE（报告期类型）

| 代码  | 含义           |
| ----- | -------------- |
| `001` | 年报           |
| `002` | 中报（半年报） |
| `003` | 一季报         |
| `004` | 三季报         |

### 过滤条件语法

多个条件用括号拼接：

```
(SECURITY_CODE="000001")(DATE_TYPE_CODE="001")
```

支持多值：

```
(DATE_TYPE_CODE="001","002")
```

### 响应格式

东方财富返回 JSONP，需要在 Go 侧去壳：

```json
jQuery1123061445791234567890_1234567890({
  "success": true,
  "result": {
    "pages": 1,
    "data": [
      {
        "REPORT_DATE": "2024-12-31 00:00:00",
        "SECURITY_CODE": "000001",
        "SECURITY_NAME_ABBR": "平安银行",
        "TOTAL_OPERATE_INCOME": 146695000000,
        "PARENT_NETPROFIT": 4450800000,
        ...
      }
    ]
  }
})
```

Go 侧处理：正则 `/jQuery.*?\((.*)\);?$/s` 提取中间 JSON，再 `json.Unmarshal`。

---

## 三大报表关键字段

### 资产负债表 (RPT_DMSK_FN_BALANCE)

| 字段名                          | 中文含义       | 英文                    |
| ------------------------------- | -------------- | ----------------------- |
| `TOTAL_ASSETS`                  | 资产总计       | Total Assets            |
| `TOTAL_LIABILITIES`             | 负债合计       | Total Liabilities       |
| `TOTAL_EQUITY`                  | 所有者权益合计 | Total Equity            |
| `TOTAL_CURRENT_ASSETS`          | 流动资产合计   | Current Assets          |
| `TOTAL_NON_CURRENT_ASSETS`      | 非流动资产合计 | Non-current Assets      |
| `TOTAL_CURRENT_LIABILITIES`     | 流动负债合计   | Current Liabilities     |
| `TOTAL_NON_CURRENT_LIABILITIES` | 非流动负债合计 | Non-current Liabilities |
| `MONETARYFUNDS`                 | 货币资金       | Cash & Equivalents      |
| `ACCOUNTS_RECEIVABLE`           | 应收账款       | Accounts Receivable     |
| `INVENTORY`                     | 存货           | Inventory               |
| `FIXED_ASSETS`                  | 固定资产       | Fixed Assets            |
| `SHORTTERM_BORROWING`           | 短期借款       | Short-term Borrowings   |
| `ACCOUNTS_PAYABLE`              | 应付账款       | Accounts Payable        |
| `LONGTERM_BORROWING`            | 长期借款       | Long-term Borrowings    |

### 利润表 (RPT_DMSK_FN_INCOME)

| 字段名                      | 中文含义         | 英文                    |
| --------------------------- | ---------------- | ----------------------- |
| `TOTAL_OPERATE_INCOME`      | 营业总收入       | Total Revenue           |
| `OPERATE_INCOME`            | 营业收入         | Operating Revenue       |
| `OPERATE_COST`              | 营业成本         | Cost of Revenue         |
| `OPERATE_PROFIT`            | 营业利润         | Operating Profit        |
| `TOTAL_PROFIT`              | 利润总额         | Total Profit            |
| `INCOME_TAX`                | 所得税           | Income Tax              |
| `PARENT_NETPROFIT`          | 归属母公司净利润 | Net Profit (Parent)     |
| `DEDUCTED_PARENT_NETPROFIT` | 扣非净利润       | Net Profit (Deducted)   |
| `SALE_EXPENSE`              | 销售费用         | Selling Expenses        |
| `MANAGE_EXPENSE`            | 管理费用         | Administrative Expenses |
| `RESEARCH_EXPENSE`          | 研发费用         | R&D Expenses            |
| `BASIC_EPS`                 | 基本每股收益     | Basic EPS               |
| `DILUTED_EPS`               | 稀释每股收益     | Diluted EPS             |

### 现金流量表 (RPT_DMSK_FN_CASHFLOW)

| 字段名                        | 中文含义                   | 英文                     |
| ----------------------------- | -------------------------- | ------------------------ |
| `NETCASH_OPERATE`             | 经营活动现金流量净额       | Net Cash from Operations |
| `NETCASH_INVEST`              | 投资活动现金流量净额       | Net Cash from Investing  |
| `NETCASH_FINANCE`             | 筹资活动现金流量净额       | Net Cash from Financing  |
| `CASH_EQUIVALENTS_INCREASE`   | 现金及等价物净增加额       | Net Change in Cash       |
| `CCE_BEGIN`                   | 期初现金余额               | Beginning Cash           |
| `CCE_END`                     | 期末现金余额               | Ending Cash              |
| `SALES_SERVICES_RECEIVE_CASH` | 销售商品提供劳务收到的现金 | Cash from Sales          |
| `PURCHASE_SERVICES_PAY_CASH`  | 购买商品接受劳务支付的现金 | Cash for Purchases       |

---

## 下载方式分析

### 方式一：结构化数据（推荐）

通过 datacenter API 获取 JSON 数据，在 `sec` 中支持以下格式导出到本地文件：

| 格式             | 说明                           | Go 实现                   |
| ---------------- | ------------------------------ | ------------------------- |
| **CSV**          | 通用表格格式，Excel 可直接打开 | `encoding/csv`（标准库）  |
| **JSON**         | 原始结构化数据                 | `encoding/json`（标准库） |
| **Excel (XLSX)** | 原生 Excel 格式                | `excelize` 第三方库       |

### 方式二：原始 PDF 年报 — 巨潮资讯网 CNINFO（已实现）

巨潮资讯网（`cninfo.com.cn`）是中国证监会指定的法定信息披露平台，覆盖沪深北三市全部 A 股上市公司。

#### CNINFO API 端点

| 端点                                                  | 方法 | 说明                               |
| ----------------------------------------------------- | ---- | ---------------------------------- |
| `https://www.cninfo.com.cn/new/data/szse_stock.json`  | GET  | 获取全部 A 股列表（含 orgId 映射） |
| `https://www.cninfo.com.cn/new/hisAnnouncement/query` | POST | 历史公告查询                       |
| `http://static.cninfo.com.cn/{adjunctUrl}`            | GET  | PDF 文件下载（公开 CDN）           |

#### 认证要求

- **无需认证**，无需 Cookie 或 Token
- 公告查询需要设置 `X-Requested-With: XMLHttpRequest` 和 `Referer` 头
- PDF 下载无任何限制

#### 公告分类代码

| 代码                  | 含义   |
| --------------------- | ------ |
| `category_ndbg_szsh`  | 年报   |
| `category_bndbg_szsh` | 半年报 |
| `category_yjdbg_szsh` | 一季报 |
| `category_sjdbg_szsh` | 三季报 |

#### 查询参数（POST body，form-urlencoded）

| 参数        | 必填 | 说明                      | 示例                    |
| ----------- | ---- | ------------------------- | ----------------------- |
| `pageNum`   | 是   | 页码                      | `1`                     |
| `pageSize`  | 是   | 每页条数                  | `30`                    |
| `column`    | 是   | 交易所：`szse`/`sse`/`bj` | `sse`                   |
| `tabName`   | 是   | 固定为 `fulltext`         | `fulltext`              |
| `plate`     | 是   | 板块：`sz`/`sh`/`bj`      | `sh`                    |
| `stock`     | 否   | 格式 `{code},{orgId}`     | `600036,gssh0600036`    |
| `category`  | 否   | 公告分类                  | `category_ndbg_szsh`    |
| `seDate`    | 否   | 日期范围                  | `2024-01-01~2024-04-30` |
| `searchkey` | 否   | 全文搜索关键词            | `年度报告`              |

#### stock → orgId 映射

通过 `szse_stock.json` 获取映射表（约 500KB，缓存 24 小时）：

| 交易所       | 代码前缀 | orgId 示例               |
| ------------ | -------- | ------------------------ |
| 深交所主板   | `00`     | `000001` → `gssz0000001` |
| 深交所创业板 | `30`     | `300750` → `gssz0300750` |
| 上交所主板   | `60`     | `600036` → `gssh0600036` |
| 上交所科创板 | `68`     | `688001` → `gssh0688001` |

#### PDF 文件命名规则

PDF 保存在 CNINFO 的静态 CDN 上，URL 格式：

```
http://static.cninfo.com.cn/{adjunctUrl}
```

其中 `adjunctUrl` 由公告查询 API 返回（如 `finalpage/2024-03-29/1205958883.PDF`）。

#### 过滤无效公告

API 返回结果可能包含摘要、英文版、更正公告等，需要通过以下条件过滤：

- `existFlag == 1`（文件存在）
- `invalidationFlag == 0`（未被撤销）
- 标题不含：`摘要`、`英文`、`已取消`、`更正`、`修订`

#### 其他官方平台对比

| 平台                | 覆盖范围        | API 格式             | 认证                      | 推荐度 |
| ------------------- | --------------- | -------------------- | ------------------------- | ------ |
| **CNINFO （巨潮）** | 沪深北全部 A 股 | form-urlencoded POST | 无                        | ★★★★★  |
| SZSE （深交所）     | 仅深市          | JSON POST            | 无（需 `X-Request-Type`） | ★★★    |
| SSE （上交所）      | 仅沪市          | GET + JSONP          | 无（需 `Referer`）        | ★★     |
| NEEQ （新三板）     | 新三板          | POST + JSONP         | 无                        | ★★     |

**结论：CNINFO 是唯一能一站式覆盖沪深北三市的官方平台，API 简洁、无认证、PDF 直链。**

---

## 实现方案

### 命令设计

新增两个命令：

**`sec balance-sheet`（别名 `sec bs`）** — 结构化财务数据查询：

```bash
# 列出所有可用财务报表
sec bs 600036

# 指定报表类型
sec bs 600036 --type balance    # 资产负债表
sec bs 600036 --type income     # 利润表
sec bs 600036 --type cashflow   # 现金流量表

# 指定报告期
sec bs 600036 --period annual   # 仅年报

# 导出到文件
sec bs 600036 --type income --output report.csv
sec bs 600036 --type balance --output report.json
```

**`sec balance-sheet-download`（别名 `sec bsd`）** — 从巨潮资讯网下载原始年报 PDF：

```bash
# 下载当年年报 PDF
sec bsd 600036

# 下载指定年份
sec bsd 600036 --year 2024

# 下载年份范围
sec bsd 600036 --start-year 2020 --end-year 2023

# 指定输出目录
sec bsd 600036 -y 2024 -o ./reports
```

### 架构设计

```
sec bs (balance-sheet.go)
    ↓
provider/eastmoney/balancesheet.go   东方财富 datacenter-web API
    → 终端表格 / CSV / JSON 输出

sec bsd (download.go)
    ↓
provider/cninfo/cninfo.go            巨潮资讯网 CNINFO
    → 公告查询 + PDF 下载到本地
    → 股票列表缓存 (~/.sec/cache/cninfo_stocks.json)
```

### 文件结构

```
provider/eastmoney/
├── eastmoney.go          # 现有 K 线接口（不改动）
├── types.go              # 新增财务数据类型
├── balancesheet.go       # 新增：财务报表获取（3 个报表共用一个获取函数）
└── balancesheet_test.go  # 单元测试

cmd/balancesheet/
├── balance-sheet.go      # sec bs — 结构化财务数据查询 + CSV/JSON 导出
├── balance-sheet_test.go # formatFieldValue / formatDate / printSummary / printDetailed 测试
├── download.go           # sec bsd — CNINFO 年报 PDF 下载
└── download_test.go      # extractYear / genStartEndDate 测试

provider/cninfo/
└── cninfo.go             # CNINFO 公告查询 + PDF 下载

utils/
└── utils.go              # SecDir() — ~/.sec/ 统一配置缓存目录
```

### 核心类型设计

```go
// FinancialReportType 报表类型
type FinancialReportType string

const (
    ReportTypeBalance   FinancialReportType = "RPT_DMSK_FN_BALANCE"
    ReportTypeIncome    FinancialReportType = "RPT_DMSK_FN_INCOME"
    ReportTypeCashFlow  FinancialReportType = "RPT_DMSK_FN_CASHFLOW"
)

// ReportPeriod 报告期类型
type ReportPeriod string

const (
    PeriodAnnual   ReportPeriod = "001" // 年报
    PeriodHalfYear ReportPeriod = "002" // 中报
    PeriodQ1       ReportPeriod = "003" // 一季报
    PeriodQ3       ReportPeriod = "004" // 三季报
)

// FinancialReportItem 单条财务报告记录（动态字段，用 map 或具体字段）
type FinancialReportItem struct {
    ReportDate   string  // 报告期
    SecurityCode string  // 证券代码
    SecurityName string  // 证券名称
    // ... 具体报表字段按需定义
}

// GetFinancialReportReq 获取财务报表请求
type GetFinancialReportReq struct {
    Code       string               // 证券代码（纯数字，如 "600036"）
    ReportType FinancialReportType  // 报表类型
    Period     ReportPeriod         // 报告期，空=全部
}

// GetFinancialReportResp 获取财务报表响应
type GetFinancialReportResp struct {
    Data []*FinancialReportItem
}
```

### 关键实现点

1. **JSONP 去壳**：东方财富返回 JSONP，需要在 `io.ReadAll` 后用正则去壳再 `json.Unmarshal`

2. **字段映射**：`ALL` 列返回大量字段（每种报表 100+ 字段），终端表格展示需要精选 10-15 个核心字段

3. **数值格式化**：财务数据以"元"为单位（如 `146695000000` = 1466.95 亿），复用 `utils.HumanNum()` 进行格式化

4. **CNINFO PDF 下载**：`sec bsd` 命令查询巨潮资讯网公告列表 → 过滤有效 PDF（排除摘要/英文/更正） → 下载到本地

5. **缓存机制**：CNINFO 股票列表缓存到 `~/.sec/cache/cninfo_stocks.json`（24h 有效期），通过 `utils.SecDir("cache")` 管理路径

6. **环境适配**：`--output` 指定 CSV/JSON 文件路径，默认输出到终端（tablewriter 表格）；PDF 下载默认输出到当前目录

### 与现有代码的复用

| 功能       | 复用来源                                                         |
| ---------- | ---------------------------------------------------------------- |
| HTTP 请求  | `utils.MakeRequest(ctx, http.MethodGet, url, nil, nil, timeout)` |
| 证券搜索   | `sina.Search(ctx, key)`                                          |
| 大数格式化 | `utils.HumanNum(value)`                                          |
| 终端表格   | `github.com/olekukonko/tablewriter`（已有依赖）                  |
| 配置目录   | `utils.SecDir("cache")` → `~/.sec/cache/`                        |

### 已实现的 Phase

1. **Phase 1：数据层**
   - `provider/eastmoney/balancesheet.go` — 东方财富结构化财务数据
   - `provider/cninfo/cninfo.go` — 巨潮资讯网公告查询 + PDF 下载
   - JSONP 解析、类型定义、错误处理
   - 单元测试完成

2. **Phase 2：CLI 命令**
   - `sec bs` — 终端表格 + CSV/JSON 导出
   - `sec bsd` — 年报 PDF 下载（`--year` / `--start-year` / `--end-year`）
   - 单元测试完成（formatFieldValue / formatDate / printSummary / printDetailed / extractYear / genStartEndDate）

3. **Phase 3：基础设施**
   - `utils.SecDir()` — `~/.sec/` 统一配置缓存目录

## 终端输出示例

```
$ sec bs 600036 --type income --period annual

证券代码 证券名称   报告期      营业总收入      营业利润        净利润
SH600036 招商银行  2024-12-31  3373.77 亿       1656.23 亿       1497.78 亿
SH600036 招商银行  2023-12-31  3383.88 亿       1714.11 亿       1572.62 亿
SH600036 招商银行  2022-12-31  3461.06 亿       1669.32 亿       1397.24 亿
SH600036 招商银行  2021-12-31  3353.11 亿       1499.82 亿       1219.55 亿
SH600036 招商银行  2020-12-31  3016.55 亿       1234.57 亿        979.59 亿
```

## CSV 导出示例

```csv
ReportDate,SecurityCode,SecurityName,TotalOperateIncome,OperateProfit,ParentNetProfit
2024-12-31,600036, 招商银行，337377000000,165623000000,149778000000
2023-12-31,600036, 招商银行，338388000000,171411000000,157262000000
```

## 设计决策

1. **东方财富 API 为主，新浪为备用**：东方财富 API 返回结构化 JSON，字段完整；新浪返回 HTML 需要 JS 渲染，不适合 CLI 工具

2. **动态字段处理**：财务数据字段多达 100+，采用 `map[string]interface{}` 存储原始数据，精选核心字段展示

3. **CSV 为首选导出格式**：通用性最好，Excel/WPS/Numbers 均可直接打开；JSON 用于程序化处理

4. **不下载原始 PDF**：PDF 来源为巨潮资讯网，需单独爬取，超出本次范围

## 参考

- 东方财富 datacenter API（公开接口，无需认证）
- GitHub: [axiaoxin-com/investool](https://github.com/axiaoxin-com/investool) — Go 语言东方财富数据中心封装
- GitHub: [akfamily/akshare](https://github.com/akfamily/akshare) — Python 金融数据接口库
- 巨潮资讯网 `cninfo.com.cn` — 上市公司法定信息披露平台（PDF 原始公告）
