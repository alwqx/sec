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

### 方式二：原始 PDF 公告（未来可扩展）

上市公司正式的财务报告 PDF 文件发布在**巨潮资讯网**（`cninfo.com.cn`），可通过爬取获取。此方案复杂度高，暂不纳入本次实现范围。

---

## 实现方案

### 命令设计

新增 `sec balance-sheet`（别名 `sec bs`）命令：

```bash
# 列出所有可用财务报表（默认近 5 年年报+最新季报）
sec bs 600036

# 指定报表类型
sec bs 600036 --type balance    # 资产负债表
sec bs 600036 --type income      # 利润表
sec bs 600036 --type cashflow    # 现金流量表

# 指定报告期
sec bs 600036 --period annual    # 仅年报
sec bs 600036 --period all       # 全部报告期

# 指定年份范围
sec bs 600036 --from 2020 --to 2025

# 下载/导出（输出到表格）
sec bs 600036 --type balance --period annual

# 导出到文件
sec bs 600036 --type income --output report.csv
sec bs 600036 --type balance --output report.json
sec bs 600036 --type cashflow --output report.xlsx
```

### 架构设计

```
cmd/balancesheet/bs.go          CLI 命令处理器
    ↓
provider/eastmoney/bs.go        财务报表数据获取（复用 datacenter-web API）
    ↓
provider/eastmoney/types.go     新增财务报表相关类型定义
    ↓
输出：tablewriter 终端表格 或 CSV/JSON/XLSX 文件
```

### 文件结构

```
provider/eastmoney/
├── eastmoney.go          # 现有 K 线接口（不改动）
├── types.go              # 新增财务数据类型
├── balancesheet.go       # 新增：财务报表获取（3 个报表共用一个获取函数）
└── balancesheet_test.go  # 单元测试

cmd/balancesheet/
├── bs.go                 # sec bs / sec balance-sheet 命令
└── bs_test.go            # CLI 测试
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

3. **数值格式化**：财务数据以"元"为单位（如 `146695000000` = 1466.95 亿），复用 `types.HumanNum()` 进行格式化

4. **下载实现**：
   - CSV：`encoding/csv` 直接输出，UTF-8 BOM 头保证 Excel 兼容
   - JSON：`json.MarshalIndent` 格式化输出
   - XLSX：可选，如需支持则引入 `github.com/xuri/excelize/v2`

5. **环境适配**：`--output` 指定文件路径，默认输出到终端（tablewriter 表格）

### 与现有代码的复用

| 功能       | 复用来源                                                         |
| ---------- | ---------------------------------------------------------------- |
| HTTP 请求  | `utils.MakeRequest(ctx, http.MethodGet, url, nil, nil, timeout)` |
| 证券搜索   | `sina.Search(ctx, key)`                                          |
| 大数格式化 | `types.HumanNum(value)`                                          |
| 终端表格   | `github.com/olekukonko/tablewriter`（已有依赖）                  |
| 日期解析   | `time.Parse(utils.LayoutYYMMDD, ...)`                            |
| 交易所映射 | `sina.BasicSecurity.ExChange` → 已在 `kline.go` 中使用           |

### 实现步骤

1. **Phase 1：数据层** (`provider/eastmoney/balancesheet.go`)
   - 实现 `GetFinancialStatements(ctx, req)` 函数
   - JSONP 解析、类型定义、错误处理
   - 单元测试

2. **Phase 2：CLI 命令** (`cmd/balancesheet/bs.go`)
   - `sec balance-sheet` / `sec bs` 命令
   - 默认表格输出（终端）
   - `--output` 导出到 CSV/JSON

3. **Phase 3：下载功能**
   - `--output report.csv` CSV 导出
   - `--output report.json` JSON 导出
   - Excel BOM 头处理

4. **Phase 4：文档**
   - `docs/balance-sheet.md`（本文件）

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
