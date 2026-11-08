# TODO

## 宏观数据采集

目标：先分别建立 CN、US 宏观数据体系，支持各自历史趋势观察，再进行中美对照。汇率与跨资产分析留待后续。

这里只列未完成的条目。已实现的指标见 [宏观指标一览](docs/macro-coverage.md)，统一导出口径见 [统一导出文档](docs/macro-standard.md)，汇率见 [汇率文档](docs/forex.md)。

### CN：中国

- [ ] 存款准备金率历史补采：内置公告覆盖 2018-01-01 起公布的调整，2018 年 1 月至 4 月中旬及 2017 年以前公告仍有缺口，见 [PBC 文档](docs/pbc.md#存款准备金率调整)。
- [ ] 服务贸易伙伴分项：继续核验可持续的月度历史来源，当前月度总量接口没有伙伴分项。
- [ ] 住户存款官方可比同比：核验可持续发布来源；全部人民币存款同比不能套给住户，当前只提供明确标记的余额计算同比。

候选来源：海关总署、国家统计局、财政部、中国人民银行。具体接口、历史覆盖与使用条件在各项实现前核验。

### US：美国

- [ ] 非农就业官方修订档案：采集 BLS 月度新增就业的首次、第二次、第三次估计，并回填官方历史岗位水平/基准修订版本；本机观测日志不可替代该档案。来源：[就业修订表](https://www.bls.gov/web/empsit/cesnaicsrev.htm)、[历史版本说明](https://www.bls.gov/web/empsit/cesvininfo.htm)。官方页面自动下载受限，待核验可持续的公开获取方式。
- [ ] 实际贸易分项净额：核验并采集商品、服务各自的官方实际净额；当前仅提供官方合计实际净出口，不以链式美元分项相减补值。
- [ ] 成屋销售长历史：核验可持续、可使用的官方历史来源；当前 FRED 公开 CSV 仅有滚动短窗口，不以缺失值或本地积累快照冒充完整档案。
- [ ] 国债逐券实际结算确认与公众净融资拆分：继续核验逐券实际到账／结算确认及剔除 SOMA 等持有人分项后的公众净融资；DTS 整体现金流不能替代逐券或持有人口径。

候选来源：BEA、美国人口普查局、美联储、美国财政部、NAR、FHFA；可评估 FRED 转接。具体接口、历史覆盖与使用条件在各项实现前核验。

### 采集基础与数据质量

`macro us` 和 `macro cn` 的指标命令都已接入 `--standard-json`，见 [统一导出文档](docs/macro-standard.md)。以下全局条目还涉及历史版本、部分抓取失败和中美可比口径，不因格式迁移完成而整体勾选。

- [ ] 国债收益率非数值标记：provider 只校验表格显示的六个期限，遇到 `--` 直接报错，`--standard-json` 只有另外五个期限按缺失处理；若接口真的返回这类标记，把校验移到表格和普通 `--json` 路径，统一导出全部按缺失处理。

- [ ] 计算值的初步标记：非农新增、时薪工时变化等计算值目前不带初步标记，需新增独立字段（如 `derived_from_preliminary`）表明当期或基期为初步估计，不改变官方水平的 `preliminary` 含义。
- [ ] 统一导出的来源与署名：将逐序列来源、发布机构和版权（如 NAR 成屋销售）放入规范字段并覆盖已迁移指标，避免丢弃 `original` 后失去署名；需确认各数据的使用条件。
- [ ] 统一元数据：地区、指标标识、所属期、数值、单位、频率。
- [ ] 明确口径：季调/非季调、名义/实际、当期/累计、指数/增速、同比/环比/环比年化。
- [ ] 分别保存公布时间、抓取时间、来源链接及修订版本；历史版本不可获得时明确标记。
- [ ] 保留可获得的原始水平值与原始响应，避免只存衍生增速。
- [ ] 缺失值与零值严格区分；标记部分抓取失败、历史缺口与数据过期。
- [ ] 正确处理中国 1—2 月合并发布，不拆分虚构月份。
- [ ] 不通过累计增速直接相减计算当月增速。
- [ ] 记录统计范围、基期和方法变化，避免跨口径连接历史序列。
- [ ] 历史分析按当时可获得的数据版本取值，避免使用未来公布或修订数据。
- [ ] 明确中美可比口径：GDP 增速、就业、消费、政策利率不直接按默认展示值排名。
- [ ] 为各指标确定历史起点、更新频率、缓存与增量更新策略。

### 汇率（高优先级）

- [ ] HKMA 历史采集及港币历史交叉核验：官方最小 GET 示例持续返回 HTTP 405 安全拦截，当前暂不接入，待公开接口可稳定访问后再实现。
- [ ] HKMA 人民币字段在岸／离岸口径核验：不能仅凭字段名 `cny` 将其认定为 CNH。
- [ ] 具有统一历史观测时点的 CNY/CNH 价差序列：新浪日线收盘时间未核实，当前只提供原始时间戳可校验的报价价差。
- [ ] TMA 基准接入需先明确数据使用授权。

### 后续阶段（本轮不实现）

先分别补齐 CN、US 数据体系；中美指标对比放到采集工作完成后最后实施。

- [ ] CN、US 分别展示历史趋势、前值及近三个月变化。
- [ ] 中美宏观指标并排对照，保留口径差异说明。
- [ ] 跨资产分析：连接宏观、汇率、股票、黄金及利率数据。

## 美股

数据优先使用 SEC EDGAR 官方接口，见 [命令文档](docs/commands.md) 和 [估值文档](docs/valuation.md)。

- [ ] 美股营业收入取错科目：蔚来（`$NIO`）FY2023 及更早的营业收入只有 1.67 亿、1.23 亿人民币，而营业成本有 525.66 亿，FY2024 起正常（657.32 亿），像是这些年份选中了一个金额很小的收入科目。`sec financials`、Desktop 财报和 `get_financials` 都受影响，`get_financials` 会算出 FY2024 营收同比 39217% 和 FY2023 上千的毛利率。核对 `provider/edgar` 收入科目的选择顺序，按年份选科目时和营业成本、毛利交叉校验。
- [x] 共用 SEC companyfacts 缓存：Desktop 的 `cachedUSFacts`（`desktop/us.go`，10 分钟）挪到 `provider/edgar`，让 `sec mcp` 的 `get_financials` 和以后的 `get_valuation` 共用；现在 `sec mcp` 每次调用都重新下载几 MB 的 companyfacts。
- [ ] Desktop 美股栏目的错误重试：财报、财报图表、分红接口对没有 XBRL 数据的公司返回 404，但前端 `ResourceState` 对所有错误都显示"重新加载"按钮，`api.ts` 的 `request()` 也不保留 HTTP 状态码，重试永远不会成功。请求失败时保留状态码，404 不显示重试按钮。
- [ ] Desktop 美股接口超时：`serveUS` 的 30 秒超时被当成上游失败（502），提示里是原始的 `context deadline exceeded`，可以改成专门的超时提示。
- [ ] （暂缓）外国公司估值，包括 IFRS 公司（台积电、诺和诺德、SAP）和以外币列报的 US GAAP 公司（阿里巴巴、ASML）。评估结论是投入大、结果难保证准确，重心先放在美国本土上市公司的估值；目前外国公司估值报"暂不支持"（`valuation.ErrForeign`）。现有做法用 ADS 价格乘 SEC 封面的普通股数，ADS 比例不是 1 时市值出错（网易 1 ADS = 5 股，估出 3871 亿美元，新浪是 774 亿；阿里巴巴封面股数疑似少一位）。调研记录：
  - 汇率：美联储 FRED 的 H.10 日度汇率覆盖所需币种（`DEXTAUS`、`DEXJPUS`、`DEXCHUS`、`DEXDNUS`、`DEXUSEU`、`DEXBZUS`、`DEXUSUK`、`DEXSDUS`、`DEXINUS` 等，多数从 1970—80 年代开始），每周更新、滞后几天，仓库已有 `provider/fred` 客户端。欧洲央行没有台币。口径上宜按当天汇率把市值折成列报币种，再和原币财报比较，不用美元便利折算数。
  - ADS 比例没有统一来源：companyfacts 的 `dei:EntityListingDepositoryReceiptRatio` 多数公司按类别标注，读不到（7 家里只有阿里巴巴有）；20-F 封面第 12(b) 条的证券名称有时写明（"each representing eight Ordinary Shares"），丰田、台积电没有；台积电要用 XBRL 实例里按 ADS 维度的每股收益反推；新浪的 ADS 折算股数可以反解当前比例，但只有当前值，且受库存股影响（丰田算出 11.07，实际为 10）。比例会随拆股变化（丰田 2021 年从 2 变为 10）。存托银行网站是 JS 页面，不适合。
  - IFRS 公司只有 20-F 年报，TTM 只能用最近财年；诺和诺德没有封面股数，要用加权股数。
- [ ] IFRS 财报补充：丰田等转用 IFRS 之前的 US GAAP 历史不显示；汇丰等在 6-K 里有半年数据，目前只整理 20-F 年报。

## 港股

- [ ] Desktop 港股栏目：港股目前只能在概览查看行情，公司、财报、估值、公告栏目都显示"此栏目暂仅支持沪深北 A 股"。实现前先调研港股财报、公告的可用数据来源。

## CLI

- [ ] `provider/sina/sina.go` 中标注待拆分的函数拆成 2 个函数。
- [ ] 港股指数行情：搜索能找到恒生指数（`HKhsi`）等港股指数，但 `sec quote` 报 `invalid HK stock quote line`，行情接口和解析都不支持港股指数。
- [ ] 搜索结果标出指数：`parseBasicSecurity` 把指数都当成 `stock`，`search_security` 只能在 `aitool/search.go` 的 `indexCode` 里按代码规则（`SH000`、`SZ399`、`BJ899` 开头、港股字母代码）判断。港股指数在新浪有单独的类型 33，可以直接用；A 股指数和股票同为 11，仍要按代码判断。把判断移到 provider，新增 `index` 类型（`types.SecurityType`），再删掉 `indexCode`。会改变 `sec search --json` 的 `security_type`，`watchlist/store.go` 目前只接受 `stock` 和 `fund`，要一起改。
- [ ] 等比复权：东方财富的前复权（`fqt=1`）把分红金额直接从更早的价格里减去，不是按比例缩小，区间越长，早期价格越接近 0 甚至为负，百分比都会失真。例如招商银行（SH600036）2002 年起的前复权月线，第一根收盘价是 −13.24；2007 年高点到 2016 年的回撤按前复权算是 −99.26%，实际 2007—2008 年约 −70%。受影响的有 `get_kline_summary`（默认前复权，目前只在 `notes` 里提示）、`sec strategy` 回测（固定前复权，长区间收益率失真）、`sec kline -f qfq`、`sec quote-history -f qfq` 和 Desktop 的 `fq=qfq`。方案：用不复权价格和分红送转数据（`sina.QueryDividends`、`eastmoney.GetDividendPlans`）按除权除息日自己算等比复权因子；做不到时，百分比统计改用不复权价格并说明除权日的跳变。先核实后复权（`fqt=2`）是不是也按金额调整。美股拆股、港股的复权方式也要一起核实。
- [ ] 巨潮证券列表没找到时重新下载：`cninfo.GetStockList` 把证券列表在 `~/.sec/cache/` 缓存 24 小时，刚上市的股票在缓存过期前查不到 orgId，`sec announce`、Desktop 公告栏目和 `list_filings` 都会报找不到（`list_filings` 目前只在报错里说明缓存）。缓存里没有这个代码时绕过缓存重新下载一次，并限制频率，避免查不存在的代码时反复下载。

## Desktop

- [x] 设置页带着旧的 SEC 联系邮箱保存会覆盖 CLI 刚改的邮箱：前端每次保存工作台（切换左侧菜单、保存其他设置）都发送打开时读到的 `sec_edgar_email`。改为设置页记录用户是否编辑过邮箱输入框，只在编辑过时发送，切换菜单等其他保存不发送；没编辑过时设置页跟随后端返回的最新邮箱。后端收到空邮箱时不修改。
- [ ] 升级或修补 `github.com/webview/webview_go` 依赖，消除新版 Clang 的 `-Wdeprecated-literal-operator` 警告：将 `operator"" _cls`、`operator"" _sel`、`operator"" _str` 改为后缀紧邻引号的写法（如 `operator""_cls`）。通过依赖升级或仓库内维护补丁处理，不直接修改 Go 模块缓存。
  - 当前这三条警告来自 WebView 的 C++ 代码，不代表构建失败；未将警告视为错误时，可正常生成二进制。临时隐藏该类警告，可在 `desktop` 目录执行：
  ```shell
  CGO_CXXFLAGS="-O2 -g -Wno-deprecated-literal-operator" go build -o sec-desktop
  ```

该参数仅隐藏警告，不替代依赖修复。详见 [Clang 诊断说明](https://clang.llvm.org/docs/DiagnosticsReference.html#wdeprecated-literal-operator)。

## 工程

- [ ] 更新 README：截图已过时，Desktop 功能列表需要补上美股公司、财报（含图表分析）、分红、估值和 SEC 申报。

## AI/LLM

原则：数字只来自 sec 的确定性计算，LLM 只负责选择调用、串联结果和解释，不产出数值；推导标记 `*`、`shares_note`、`dividends_note` 等口径说明和数据截止日期原样交给模型，并要求回答保留；只解释数据，不给买卖建议（国内证券投资咨询需持牌）。agent mode 调研（[#99](https://github.com/alwqx/sec/issues/99)）并入本节推进。

设计方案见 [AI/LLM 接入设计](docs/ai.md)。按以下顺序拆分，一项一个 PR：

- [x] Agent Skill 与文档（[使用说明](docs/agent-skill.md)）：写一份 SKILL.md，说明如何在 Claude Code、Cursor 等工具里配合 sec CLI 使用：各命令用 `--json`，macro 命令用 `--standard-json`；不改 Go 代码。
- [x] `sec mcp`：stdio MCP server，第一个 PR 先提供 7 类只读工具（search、quote、kline 摘要、financials、valuation、filings / announcements、macro），复用现有 provider；工具输出另定精简格式，避免把 10 年财报 JSON 原样塞给模型，每条结果带来源、截止日期和口径说明，并直接给出同比、变化量等派生值，不让模型自己计算。工具注册表独立成 `aitool/` 包，放在 `cmd/`、`desktop/` 之外，保证跨平台编译，供 `sec ask` 和 Desktop 对话复用。一个工具一个 PR，添加方法见 [sec mcp 文档](docs/mcp.md#添加工具)：
  - [x] 框架与 `get_quote`：`aitool` 注册表和输出信封、`sec mcp` stdio server、最新行情工具。
  - [x] `search_security`：搜索证券代码。
  - [x] 重构：并发搜索去重抽成 `sina.ResolveAll`，涨跌计算抽成 `SecurityQuote.Change`，`cmd/quote`、`cmd/watch` 和 `aitool` 共用。
  - [x] 基金净值的 `volume`、`amount` 在 `sec quote --json` 里也改成 `null`，和 `get_quote` 一致。
  - [x] Desktop 行情接口（`/api/v1/quote`、`/api/v1/quotes`）的基金净值 `volume`、`turnover` 改成 `null`，行情卡片和自选页显示 —。
  - [x] 搜到同一只证券的关键字（如 `600036,cmb`）在 `sec quote` 表格、`--json` 和 `get_quote` 里都合并为一条并说明，说明文字由 `sina.MergeNote` 统一生成。
  - [x] `get_kline_summary`：K 线区间统计和最近 20 根。
  - [x] `get_financials`：最近 5 年或 8 个季度的关键财务项及同比，A 股和美股统一结构。
  - [x] `get_valuation`：估值、历史分位、`assessment`、全部 `_note` 和 `disclaimer`。
  - [x] `list_filings`：巨潮公告和 SEC 申报列表。
  - [x] `get_macro_series`：宏观指标的口径和最近 12 期观测。
- [ ] 共用 AI 配置：CLI 和 Desktop 共用 `~/.sec/config.json` 的 `ai` 段，可以保存多个模型配置（`profiles`，按名字区分，各自有厂商 `provider`、接口协议 `api`、`base_url`、模型和 Key，协议和地址默认用厂商的官方设置），`default` 指定默认配置；`sec config ai list/add/edit/set-key/use/remove` 管理；Key 可以用环境变量，或用 AES-256-GCM 加密保存在 `config.json`，加密密钥单独存在 `~/.sec/secret.key`（`0600`）；`set-key` 隐藏输入或从 stdin 读取，不接受命令行参数。拆成以下几个 PR：
  - [x] `config` 包的 `ai` 段：结构、内置厂商列表、校验、旧文件兼容、增删改和设默认值，`sec config` 显示摘要且不显示 Key。
  - [x] Key 加密与读取：`secret.key`、AES-256-GCM（配置名作附加认证数据）、`enc:v1:` 格式、Key 读取顺序，显示时给出来源和末 4 位。
  - [ ] `sec config ai list/add/edit/use/remove/enable/disable`。
  - [ ] `sec config ai set-key/unset-key`：终端里隐藏输入，管道时从 stdin 读一行。
  - [x] `ai` 段无效时的两处问题：`config.UpdateFile` 拒绝一切写入，Desktop 前端每次保存都带 `sec_edgar_email`，所以每一次保存（包括自选股）都报 500「请稍后重试」。改为不修改 `ai` 段时照常保存其他配置、`ai` 段按原样写回，只有修改 `ai` 段时才返回 `ErrInvalidAI`；`sec mcp` 的 `secClient` 读取配置出错时都记「使用默认 SEC 联系邮箱」，`ai` 段无效时实际用的是配置里的邮箱，日志应只说读取配置有问题并带上原因（`aitool/financials.go`）。
- [ ] `llm/` 模型接入抽象：支持 Anthropic 和 OpenAI 兼容接口（覆盖 DeepSeek、通义、本地 Ollama），含回答数字核对、`sec config ai test` 测试连接和各命令的 `--ai <配置名>` 参数；CLI 和 Desktop 共用。
- [ ] Desktop 模型设置：设置页管理同一份 `ai` 配置（新增、编辑、删除、设为默认）、保存 Key、测试连接，AI 功能旁边可以临时切换配置；说明自选股和查询内容会发给模型厂商，本地模型可不出本机。
- [ ] 估值解读：`sec valuation <code> --explain`，把估值精简输出和口径说明交给模型生成一段说明，范围有限、便于测试。
- [ ] Desktop 估值 tab「解读」按钮：复用上一项。
- [ ] 申报与公告摘要：CLI 用 `--summary`，Desktop 在列表行上加按钮，两边共用 `llm/` 里的实现。拆成以下几个 PR：
  - [ ] 巨潮公告摘要。
  - [ ] 美股 8-K 事项摘要。
  - [ ] 10-K 的 Risk Factors 和 MD&A 摘要，逐年对比风险因素的增删。
  - [ ] 招股书 PDF 问答（`sec ipo ask`），回答附页码引用。
- [ ] `sec ask`：单次问答，agent loop 复用 `aitool/` 工具注册表；混合多个工具结果时带上各自截止日期，避免拼接不同时点的数据。
- [ ] Desktop 对话面板：复用 `sec ask` 的 agent loop。

测试：CI 不调用真实模型，用假的模型客户端测试工具调用与提示词组装；回答质量用离线评测集手动运行。

## 调研

- [ ] 专业金融数据源（[#73](https://github.com/alwqx/sec/issues/73)）：评估 tickflow.org 等来源的数据范围和使用条件。
- [ ] 新浪财经接口梳理（[#23](https://github.com/alwqx/sec/issues/23)）、数据输出方案（[#22](https://github.com/alwqx/sec/issues/22)）：2024 年开的，大部分已经实现（新浪接口已在使用，有 Desktop 和终端表格），核对后决定是否关闭。
