---
name: sec
description: 用 sec 命令行查询 A 股、港股、美股、基金的行情、K 线、财报、估值、分红、公告、SEC 申报、IPO，以及中美宏观、汇率、美债、贵金属和大宗商品数据，并按数据如实解释。用户问到股票或基金的价格、涨跌、财务、估值、公告，或 CPI、GDP、利率、汇率等宏观数据时使用。Use the sec CLI to look up Chinese, Hong Kong and US market data, financials, valuation, filings and macro data, and explain it without giving investment advice.
---

# sec：证券与宏观数据查询

`sec` 是一个终端命令，从新浪财经、东方财富、巨潮资讯、SEC EDGAR 和各国官方统计机构取数。你通过它的 `--json` 输出拿数据，再用中文向用户解释。

## 基本原则

1. **数字只来自 sec**：回答里的每个数值都必须能在 sec 的输出里找到。不要凭记忆补数据，不要估算缺失值。sec 没有给出的数值就说「sec 的数据中没有」。
2. **尽量不自己计算**：同比、分位、增长率、股息率等优先用 sec 已经给出的字段。用户明确要求而 sec 没有直接给出时，可以做简单计算，但要写明「按 sec 数据计算」和公式。
3. **保留口径说明**：以 `_note` 结尾的字段（`source_note`、`shares_note`、`dividends_note`、`peers_note` 等）非空时，要和对应数值一起告诉用户。`sec quote --json` 顶层的 `notes` 说明哪些关键字搜到同一只证券、已合并为一条。财报里标 `*` 的是推导值，要说明。`null` 表示上游缺失或不适用，不能说成 0。
4. **写明数据日期**：说出数据的截止日期（行情的 `trade_date` 和 `time`、财报报告期、宏观统计期）。把不同日期的数据放在一起时分别说明，不要当成同一时点。
5. **不给投资建议**：只解释数据，不说买入、卖出、持有、加减仓或目标价。`sec valuation` 的 `assessment`（偏低区间、合理区间、偏高区间）是规则机械判定，引用时说明这一点，并带上 `disclaimer`。

## 使用前

- 先运行 `sec --version` 确认已安装。没有安装时告诉用户去 <https://github.com/alwqx/sec/releases> 下载，不要改用其他数据源编造结果。
- 需要联网。美股的 SEC 数据要求配置联系邮箱：返回 403 时提示用户运行 `sec config set sec-edgar-email you@example.com`。
- 一律加 `--json`。不要用 `sec quote -r` 和 `sec kline` 的图表模式，它们是给人看的交互输出。

## 证券代码

- A 股：`600519` 或带交易所前缀的 `SH600519`、`SZ000001`、`BJ430047`。
- 港股：`00700` 或 `HK00700`。
- 美股：`$AAPL`。在 shell 里要加单引号：`'$AAPL'`，否则 `$AAPL` 会被当成变量。`sec filings` 直接写 `AAPL`。
- 不确定代码时先 `sec search <名称或拼音> --json`，从 `results` 里取 `ex_code`。有多个结果时向用户确认，不要自己挑。

## 常用命令

| 需求 | 命令 |
| --- | --- |
| 搜索代码 | `sec search 茅台 --json` |
| 实时行情（最多 5 只，逗号分隔） | `sec quote '600519,00700,$AAPL' --json` |
| 公司资料、核心指标 | `sec info 600519 --json` |
| 分红 | `sec info 600519 -d --json` |
| 历史行情 | `sec quote-history 600519 -b 20260101 -e 20261031 --freq week --json` |
| K 线数据 | `sec kline 600519 -b 20260101 --json`（`--fq qfq` 前复权） |
| 财报 | `sec financials 600519 --type income --period annual --json`；`--type` 取 `balance`、`income`、`cashflow` |
| 估值 | `sec valuation 600519 --json`（A 股和美股） |
| 技术指标 | `sec strategy macd 600519 --json`，另有 `ma`、`rsi`、`boll` |
| A 股公告 | `sec announcements 600519 --type annual --json` |
| 增减持公告 | `sec insider 600519 --json` |
| 美股 SEC 申报 | `sec filings AAPL --form 10-K,8-K -n 10 --json` |
| IPO | `sec ipo list --json`、`sec ipo calendar --json`；招股书链接 `sec ipo prospectus <代码>`（没有 `--json`） |
| 中国宏观 | `sec macro cn cpi -b 202601 -e 202609 --standard-json` |
| 美国宏观 | `sec macro us payrolls -b 202501 -e 202609 --standard-json` |
| 汇率 | `sec fx quote --pairs USD/CNY,USD/CNH --json`、`sec fx history --pairs USD/CNH -b 20260101 --json` |
| 美债收益率 | `sec bond --json`、`sec bond-history -b 20260101 --json` |
| 贵金属、大宗商品 | `sec metal --json`、`sec commodity --json` |

宏观指标很多，先用 `sec macro cn --help`、`sec macro us --help` 查看指标名，再用 `sec macro cn <指标> --help` 查看日期格式（月度 `YYYYMM`、季度 `2026Q1`、日度 `YYYYMMDD`）和可选参数。宏观数据优先用 `--standard-json`：它带 `schema_version`，每条观测关联明确的口径（频率、季调、价格基准、官方值还是计算值），见下文。个别指标只有 `--json`。

不确定参数时运行 `sec <命令> --help`，不要猜。

## 读取 JSON

- **成功与失败**：退出码非 0 时不要解析 stdout，把 stderr 里的错误原样告诉用户。`sec quote --json` 有任何一只查不到就整体失败，可以拆开分别查。
- **输出可能很长**：`sec valuation --json` 带每天的历史估值序列，`kline`、`quote-history`、`financials` 也可能有几百行。用 `jq` 或脚本只取需要的字段，不要把整份 JSON 读进上下文。例如：

  ```bash
  sec valuation 600519 --json | jq '{name, trade_date, price, pe: {current: .pe.current, history: .pe.history}, pb: .pb.current, ps: .ps.current, assessment, shares_note, dividends_note, peers_note, disclaimer}'
  ```

- **字段约定**：snake_case，日期是 `YYYY-MM-DD`，列表在 `results` 里（`fx` 用 `observations`）。涨跌幅、换手率的单位是 %。证券代码字段多数叫 `ex_code`，`valuation` 叫 `code`。
- **A 股和美股结构不同**：`info`、`financials` 的美股输出是另一套结构。字段含义不确定时查 [JSON 输出约定](https://github.com/alwqx/sec/blob/main/docs/json.md)。
- **行情时间**：`trade_date` 和 `time` 是当地时间，时区在 `timezone`。基金的 `nav` 为 `true` 时，价格是单位净值，不是成交价；这时没有成交，`sec quote --json` 的 `volume`、`amount` 是 `null`，汇总成交额时要跳过，不能当成 0。
- **宏观统一格式**：`--standard-json` 的 `series` 说明每条序列的口径，`observations` 通过 `series_id` 关联到 `series`。解释时用 `series` 里的口径（`seasonal_adjustment`、`value_origin`、`coverage` 等），比如区分单月同比和累计同比、季调和未季调。观测的 `status` 不是 `available` 时看 `missing_reason`。`original` 是适配前的原始结果，通常很长，用 `jq 'del(.original)'` 去掉。

## 回答格式

- 先直接回答问题，再列出关键数字和日期，最后写口径说明和数据来源。
- 引用估值时：当前 PE/PB/PS、历史分位、`assessment`（注明是规则判定），以及非空的 `_note` 和 `disclaimer`。
- 比较多只证券或多个时点时用表格，每列写明日期。
- 命令失败或数据缺失时如实说明，不要换个说法掩盖。

## 示例

用户问：「茅台现在估值贵吗？」

1. `sec search 茅台 --json` 得到 `SH600519`。
2. 用上面的 `jq` 命令取估值的关键字段。
3. 回答：「截至 2026-11-06，贵州茅台 PE 22.1 倍，处于 2018 年以来的 19% 分位（历史中位数 32.2 倍）。sec 按规则判定为『偏低区间』，这是机械计算的结果，不构成投资建议。」然后补上非空的 `_note`。
