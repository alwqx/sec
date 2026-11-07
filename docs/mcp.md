# sec mcp

`sec mcp` 通过 stdio 运行一个只读的 [MCP](https://modelcontextprotocol.io) server，让 Claude Code、Cursor、Codex 等 AI agent 直接调用 sec 的工具取数。模型由 agent 自己提供，sec 只提供工具，不需要配置模型或 API Key。设计背景见 [AI/LLM 接入设计](ai.md)。

和 [Agent Skill](agent-skill.md) 的区别：Skill 让 agent 运行 `sec ... --json` 命令并自己读输出；MCP 工具的输出是专门给模型精简过的格式，带截止日期和口径说明，不需要 agent 能运行 shell 命令。

## 配置

先确认 `sec` 在 `PATH` 里，`sec --version` 能运行。

**Claude Code**：

```bash
claude mcp add sec -- sec mcp
```

**Cursor**：在 `~/.cursor/mcp.json`（全局）或项目的 `.cursor/mcp.json` 里加：

```json
{
  "mcpServers": {
    "sec": { "command": "sec", "args": ["mcp"] }
  }
}
```

**Codex**：在 `~/.codex/config.toml` 里加：

```toml
[mcp_servers.sec]
command = "sec"
args = ["mcp"]
```

其他支持 stdio MCP server 的工具同理：命令是 `sec`，参数是 `mcp`。`sec` 不在 `PATH` 里时写完整路径。

## 工具

| 工具 | 说明 |
| --- | --- |
| `search_security` | 按代码、名称或拼音搜索 A 股、港股、美股、指数和基金，返回 `ex_code`、名称、市场和类型，最多 10 条 |
| `get_quote` | A 股、港股、美股和基金的最新行情，一次 1 到 5 只，代码或名称都可以 |

其余工具（K 线摘要、财报、估值、公告与申报、宏观等）按 [TODO](../TODO.md) 的 AI/LLM 部分逐个加入。

## 输出格式

每个工具都返回同一个信封，序列化成一段 JSON 文本：

```json
{
  "schema_version": "1",
  "tool": "get_quote",
  "as_of": "2026-11-06",
  "source": "新浪财经行情",
  "notes": [],
  "data": { "quotes": [ ... ], "missing": [] }
}
```

| 字段 | 说明 |
| --- | --- |
| `schema_version` | 信封和 `data` 的格式版本。删除、重命名字段或改变含义时加 1，新增字段不变 |
| `as_of` | 数据本身的截止日期（交易日、报告期或统计期），不是查询时间。数据不是同一个时点时省略，以 `data` 里各条的日期为准，比如同时查 A 股和美股 |
| `source` | 数据来源 |
| `notes` | 口径说明、推导说明、截短说明，回答时应保留；没有时是空数组 |
| `disclaimer` | 结果自带的免责声明，没有时省略 |
| `data` | 工具自己的数据，`null` 表示缺失或不适用，不是 0 |

工具出错时返回 `isError: true` 的结果，文本是中文原因：参数错误（比如一次超过 5 只，或者给出的代码全都找不到）提示怎么改，上游失败或超时说明哪一步失败。以 `get_quote` 为例，部分证券查不到不算出错，列在 `data.missing` 里；几个输入搜到同一只证券时合并为一条，并在 `notes` 里说明；基金净值没有成交，`volume`、`amount` 是 `null`。`search_security` 没有匹配时不算出错，返回空的 `results` 并在 `notes` 里提示换关键字；结果超过 10 条时截短，`total` 是新浪返回的条数（接口最多约 50 条，不是匹配总数）。`market` 是 `sse`、`szse`、`bse`、`hk`、`us` 或 `fund`，基金不区分场内场外。

server 连接时会给模型一段使用规则（只用工具返回的数字、保留口径说明和日期、不给投资建议），见 `cmd/mcp/mcp.go` 的 `instructions`。

## 日志与排查

stdout 只给 MCP 协议用，日志写到 stderr，默认只记警告和错误；`sec mcp -D` 输出调试日志。单次工具调用 30 秒超时。

仓库里的 `scripts/mcp-smoke.py` 只用 Python 标准库，按 JSON-RPC 走一遍握手、列出工具和调用，不需要 npm：

```bash
go build -o sec . && scripts/mcp-smoke.py ./sec        # 每个工具用示例参数和空参数各调用一次，检查信封格式和大小
scripts/mcp-smoke.py ./sec --call get_quote '{"codes": ["600519", "$AAPL"]}'   # 只调用一个工具，打印完整结果
```

有失败时退出 1。它需要联网，只在本地手动运行，不进 CI。

用官方的 Inspector 可以在网页里手动列出和调用工具：

```bash
npx @modelcontextprotocol/inspector sec mcp
```

## 添加工具

工具都在 [`aitool/`](../aitool) 包里，`sec mcp` 和以后的 `sec ask`、Desktop 对话共用。这个包不能依赖 `cmd/`、`desktop/` 和任何 MCP 或模型 SDK；MCP 的适配只在 `cmd/mcp`。以 `get_quote`（[`aitool/quote.go`](../aitool/quote.go)）为例，新增一个工具：

1. **新建文件** `aitool/<名字>.go`，写一个返回 `Tool` 的函数，比如 `ValuationTool()`：
   - `Name`：英文 snake_case，以动词开头，比如 `get_valuation`、`list_filings`。
   - `Description`：中文，写清楚做什么、什么时候用、返回什么、有什么限制。模型靠它决定调用哪个工具。
   - `InputSchema`：参数的 JSON Schema，顶层是 `object`，写 `required` 和 `"additionalProperties": false`，每个参数带 `description` 和示例写法。
   - `Call`：实现。
2. **解析参数**：用 `Decode(input, &in)`，它拒绝不认识的字段。必填项、取值范围自己检查，不合法时返回 `Inputf(...)`，说明怎么改。
3. **取数**：调用现有的 provider 或计算包（如 `valuation.Analyze`），不要调用 `cmd/` 里的函数；需要的逻辑在 `cmd/` 里时，先把它挪到 provider 或业务包。上游失败返回普通 `error`，写清楚哪一步失败。
4. **精简输出**：`data` 只放回答问题需要的字段，不要原样转发 `--json` 的结构。
   - 每个结果控制在 4 KB 左右：长历史只保留最近几期，或给出区间统计（首尾、最高最低、均值、分位）；截短了就在 `notes` 里写明。
   - 同比、变化量、分位等派生值在 Go 里算好并四舍五入，不让模型自己算。
   - 缺失的值用指针表示 `null`，不用 0。
5. **填写信封**：用 `NewResult(name, source, data)`；设置 `AsOf`；把非空的 `*_note`、推导说明和截短说明放进 `Notes`；估值等自带免责声明的放进 `Disclaimer`。
6. **注册**：在 `aitool.go` 的 `Default()` 里加上新工具。
7. **测试**：在 `aitool/<名字>_test.go` 里用 `gock` 模拟上游，覆盖正常结果、`notes`、`as_of`、参数错误和上游失败；`cmd/mcp` 的测试会自动检查新工具能被列出。再在 `scripts/mcp-smoke.py` 的 `SAMPLES` 里加一条示例参数，用真实数据跑一遍，看输出大小和内容。
8. **文档**：在上面的工具表里加一行，CHANGELOG 加一条。
