# JSON 输出约定

本文说明 `--json` 输出哪些部分是稳定的、怎样算破坏性变更、消费者该怎样读。基本约定（stdout、snake_case、`null`、失败时 stdout 为空）见 [命令文档的脚本调用](commands.md#脚本调用)，宏观统一导出 `--standard-json` 有自己的版本号和字段，见 [统一导出文档](macro-standard.md)。

## 稳定性

稳定的是已有字段的名称、类型、单位和含义，以及顶层结构。

- **兼容变更**：新增字段、新增可选的说明字段（如 `*_note`）。不另行标注，消费者应忽略不认识的字段。
- **破坏性变更**：删除或重命名字段，改变类型、单位、货币、时区或含义，改变顶层结构，把原来总有值的字段改成可能为 `null`。这类变更在 CHANGELOG 对应条目里用 **JSON change:** 标出，并写明新旧行为。
- 字段顺序、缩进和数字的小数位数不属于约定。

## 版本

- `--json` 的每份输出不带版本号。约定的版本就是 sec 的版本（`sec --version`），两个版本之间的破坏性变更在 CHANGELOG 里搜 `JSON change`。
- `--standard-json` 带 `schema_version`，按 [统一导出文档](macro-standard.md) 检查。
- 后续的 `sec mcp`（见 [TODO](../TODO.md) 的 AI/LLM 部分）会输出自己的精简格式并带版本号，不直接暴露 `--json` 的结构。

## 怎样读

- 按字段名取值，忽略未知字段；`null` 表示上游缺失或不适用，不能当 0。
- 日期是 `YYYY-MM-DD`；行情时间的时区见 [行情文档](quote.md)。
- 以 `_note` 结尾的字段（如 `source_note`、`shares_note`、`dividends_note`）是口径说明或备用数据提示，非空时应和数值一起展示。财报里标 `*` 的值是推导值，见各命令文档。
- 退出码非 0 时不要解析 stdout。

## 各命令的顶层结构

| 命令 | 顶层字段 | 说明 |
| --- | --- | --- |
| `search` | `results` | 每项 `code`、`ex_code`、`exchange`、`name`、`security_type` |
| `quote` | `results` | 每项一只证券的行情 |
| `info`（A 股、港股） | `ex_code`、`name`、`history_name`、`listing_date` 等公司字段 | 一个对象，不在 `results` 里；`description` 是公司简介，目前只有港股有 |
| `info`（美股） | `ex_code`、`ticker`、`company`、`quote` | `company` 或 `quote` 取不到时为 `null` |
| `kline`、`quote-history` | `ex_code`、`name`、`freq`、`fq`、`source_note`、`results` | `results` 每项一根 K 线 |
| `financials`（A 股） | `ex_code`、`name`、`results` | |
| `financials`（美股） | `ex_code`、`ticker`、`name`、`cik`、`standard`、`quarterly`、`statements` | 结构和 A 股不同 |
| `valuation` | `code`、`currency`、`name`、`trade_date`、`price`、`pe`、`pb`、`ps` 等 | 一个对象；`peers`、`dividends` 取不到时为 `null`，原因在对应的 `_note` |
| `announcements` | `results`、`page`、`total`、`total_pages` | |
| `filings` | `ticker`、`company`、`results`、`truncated` | |
| `insider` | `ex_code`、`name`、`results`、`truncated` | |
| `ipo list`、`bond`、`bond-history`、`commodity` | `results` | |
| `metal` | `inst`、`name`、`unit`、`results` | |
| `fx` 各子命令 | `provider`、`timezone`、`observations`、`notes` 等 | 用 `observations`，不用 `results` |
| `macro us`、`macro cn` 各指标 | `region`、`indicator`、`frequency`、`unit` 等 | 各指标不同，见对应文档 |

## 已知的不一致

下面这些是历史原因留下的，改动都属于破坏性变更，暂时保留。新命令不要再这样写。

- **证券代码的字段名**：多数命令是 `ex_code`（如 `SH600519`），`valuation` 是 `code`，值的格式和 `ex_code` 相同。
- **A 股和美股结构不同**：`info`、`financials` 的美股输出是另一套结构，见上表。
- **成交量和成交额**：`sec quote --json` 的 `volume` 是成交量（股或份）、`amount` 是成交额；Desktop 的行情接口直接输出内部结构，`volume` 是成交额、`turnover` 是成交量，两者正好相反。
- **列表字段**：`fx` 用 `observations`，其他命令用 `results`。
