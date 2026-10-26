# 国家统计局宏观数据采集

## NBS 的含义

`NBS` 是 **National Bureau of Statistics** 的缩写，即中国国家统计局。`provider/nbs` 按数据来源命名，与 `provider/sina`、`provider/eastmoney` 一致，仅负责国家统计局数据的请求、解析和缓存。

- 国家统计局官网：<https://www.stats.gov.cn/>
- 官方数据发布库：<https://data.stats.gov.cn/>
- 新版月度查询页面：<https://data.stats.gov.cn/dg/website/page.html#/pc/national/monthData>
- CPI 编制方法：<https://www.stats.gov.cn/zs/tjws/zytjzbqs/jmxxggzs/202411/t20241127_1957589.html>

## 当前范围与用法

目前支持全国月度 CPI、核心 CPI（不包括食品和能源）、PPI（工业生产者出厂价格指数），分别查询同比与环比口径；另支持制造业 PMI 及四个分项、非制造业商务活动指数及相关分项，口径见下文。暂不涉及 US、汇率、股票与黄金的联合分析，也未接入 Desktop。

```bash
sec macro cn cpi
sec macro cn core-cpi
sec macro cn ppi

sec macro cn cpi --begin 202001 --end 202609
sec macro cn core-cpi --begin 202601 --end 202609 --json
sec macro cn ppi --refresh
sec macro cn cpi --no-cache
```

- 月份参数为 `YYYYMM`；默认截至上一个自然月，开始月份默认为截至月份前 11 个月。
- 上个月的数据可能尚未发布，输出只包含接口实际返回的月份，不将缺失月份填为零。
- `--json` 输出原始指数、衍生涨跌幅、地区、指标、月份、比较口径、目录 ID、指标 ID、官方指标名称、来源和抓取时间；缺失数值为 `null`。
- `--refresh` 跳过现有缓存并重新采集；`--no-cache` 完全禁用磁盘缓存。
- 核心 CPI 使用独立指标，不以“非食品 CPI”替代。核验时 2021—2025、2026 年起的目录包含该指标，2016—2020 目录未提供。此类历史缺口输出空值，并在 `note` / 表格说明列标记“该历史目录未提供核心 CPI 指标”；这不表示当时没有发布过核心 CPI。

## 新版接口

2026-10-26 核验时，官网已迁移到新版发布库，旧版 `easyquery.htm` 在当前环境返回 HTTP 403。本实现沿用官方新版页面发出的请求，不依赖旧版 `A0101` 等指标编码。

接口基址：

```text
https://data.stats.gov.cn/dg/website/publicrelease/web/external
```

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/new/queryIndexTreeAsync?code=1&pid=...` | 发现月度数据、价格指数和历史分段目录；根目录省略 pid |
| GET | `/new/queryIndicatorsByCid?cid=...&pageSize=1000` | 获取目录下的指标元数据，精确匹配总体/核心指标及比较口径 |
| POST | `/stream/esData` | 获取全国月度观测值，读取完整响应后解析 JSON |

数据请求体结构：

```json
{
  "cid": "所选目录 ID",
  "rootId": "价格指数目录 ID",
  "indicatorIds": ["所选指标 ID"],
  "daCatalogId": "",
  "das": [{"text": "全国", "value": "000000000000"}],
  "dts": ["202601MM-202609MM"],
  "showType": 1
}
```

月份必须带 `MM` 后缀。CPI 目录按 `(-2015)`、`(2016-2020)`、`(2021-2025)`、`(2026-)` 等区间分段；程序动态发现目录，只查询与所选时间范围相交的分段。即使底层序列返回了目录区间之外的数据，也按分段范围裁剪，避免重复月份。目录/指标结构改变、指标列表被截断、重复观测或无效数值均返回错误，不把部分结果当完整结果输出。

这些是官网使用的接口，并非具有版本兼容承诺的公开开发者 API；目录名称或响应结构变化时需要重新核验。

## 统计口径

| `basis` | 原始指数含义 | `change_percent` |
| --- | --- | --- |
| `上年同月=100` | 同比相对指数 | 原始指数减 100，单位 % |
| `上月=100` | 环比相对指数 | 原始指数减 100，单位 % |

例如原始指数 `100.9` 对应涨幅 `0.9%`，不是涨幅 `100.9%`。这两类相对指数也不是可直接计算长期累计价格变化的固定基期指数。本实现未进行季节调整，不计算环比年化，不混用“上年同期=100”的累计口径。

部分官方元数据将同比指数单位标为 `%`，仍以官方指标名称中的比较基准解释其数值，不直接视为百分比增速。历史分段的分类与权重可能变化，输出保留目录与序列标识，供后续分析追溯。

接口中的 `null`、空字符串、`--` 等缺失标记转为 `nil`，真实 `0` 保留为数值。没有返回的月份不插值；返回月份缺少所选全国指标时保留空值。

## 缓存与时间信息

缓存位于 `~/.sec/cache/nbs/`，按请求地址与请求体的 SHA-256 存储，六小时内复用。缓存含完整原始 JSON 响应与抓取时间，写入使用临时文件加原子重命名；失败请求不覆盖成功缓存。

当前采用按查询范围缓存、过期后重新获取的策略，**尚未实现按序列增量合并、修订版本归档或公布日历**。刷新同一个请求会替换它的旧缓存。

`period` 是数据所属月份，`fetched_at` 是实际抓取时间（UTC），缓存命中时保留原抓取时间。接口未返回逐条公布时间，程序不将所属月份、元数据中的日期范围或抓取时间伪装成公布时间。本版本不支持重建“历史某日当时已知的数据”，不能据此开展无前视偏差的回测。

## 代码与验证

```text
provider/nbs/nbs.go          请求、错误处理、原始响应缓存
provider/nbs/prices.go       目录发现、历史分段、价格指标解析
provider/nbs/pmi.go          制造业、非制造业目录发现与调查指数采集
provider/nbs/prices_test.go  官方响应样本与本地 HTTP 模拟测试
provider/nbs/testdata/       核验时获取的官方响应样本（部分精简）
cmd/macro/macro.go          命令参数、JSON 与中文表格
cmd/macro/pmi.go            PMI 命令与月度五项指数表格
cmd/macro/nonmanufacturing.go 非制造业总览与分项明细
```

provider 接收 `context.Context`，支持取消与请求超时；CLI 通过 `cmd.OutOrStdout()` 输出。`nbs.NewClient("")` 可在其他调用方禁用磁盘缓存。

```bash
go test ./provider/nbs ./cmd/macro
go test ./...
```

离线测试覆盖三类指标、同比/环比、原始指数、真实零涨幅、缺失值、历史分段筛选、缓存命中/刷新、上游错误、取消请求和月份校验。真实网络验证应手动运行上述命令，不加入 CI。

## 制造业 PMI

`PMI` 是 Purchasing Managers' Index（采购经理指数）的缩写。`pmi` 命令采集全国制造业总指数和四个指定分项；非制造业商务活动指数使用下文的独立命令，综合 PMI 尚未接入。

```bash
sec macro cn pmi
sec macro cn pmi --begin 202001 --end 202609
sec macro cn pmi --begin 202607 --end 202609 --json
sec macro cn pmi --refresh
sec macro cn pmi --no-cache
```

月份默认范围、缓存目录、六小时有效期和刷新方式与价格查询一致。终端每个月展示一行，含总指数、生产、新订单、新出口订单、从业人员五列；JSON 每个月输出五条观测记录。

| JSON `indicator` | 官方指标 | 含义 |
| --- | --- | --- |
| `manufacturing-pmi` | 制造业采购经理指数 | 制造业总体景气 |
| `production` | 生产指数 | 生产活动 |
| `new-orders` | 新订单指数 | 新增需求 |
| `new-export-orders` | 新出口订单指数 | 出口订单需求 |
| `employment` | 从业人员指数 | 企业用工变化 |

采集流程为“月度数据 → 采购经理指数 → 制造业采购经理指数 → 指标元数据 → 五项全国观测值”。动态查找目录和指标 ID，并在同一数据请求中提交五个 ID。不会误选非制造业或综合 PMI，也不自动扩展到其余分项。

PMI 是扩散指数，以百分数形式发布，取值范围为 0—100。官方数据已经季节调整，JSON 显式返回 `unit: "%"` 和 `seasonally_adjusted: true`。50 是临界点，大于/小于 50 表示相应调查活动相对上月扩张/收缩，**不表示产出同比或环比增长了该百分比**。它不适用价格指数的“减 100”转换；本次也不计算月差或经济状态评分。方法和季调说明见[国家统计局 PMI 发布说明](https://www.stats.gov.cn/sj/zxfb/202609/t20260930_1965449.html)。

JSON 保留全国地区、指标键、所属月份、原始指数、官方目录/序列标识、名称、来源和抓取时间。缺失分项输出 `null`，表格显示 `—`；整个月未返回时不生成虚构数据。重复月份/观测值、非法或超出 0—100 的数值、元数据缺少必需指标、指标列表被截断时返回错误。公布时间和历史修订归档限制与上文一致。

采购经理调查 JSON 增加 `survey` 字段：制造业为 `manufacturing`，非制造业为 `nonmanufacturing`。同名分项（如 `new-orders`）应结合 `region`、`survey`、`indicator`、`period` 识别，不能跨调查合并为同一序列。制造业原有指标键和数值不变。

`pmi-catalogs.json`、`pmi-series.json`、`pmi-values.json` 是 2026-10-26 从官方接口取得并精简的测试样本，数据样本覆盖 2026 年 7—9 月。测试覆盖五项正确映射、月份排序/过滤、零值与缺失值、无效/重复响应、缓存命中与刷新、CLI 月份校验和中文表格展示。

## 非制造业商务活动指数

```bash
sec macro cn nonmanufacturing-pmi
sec macro cn npmi --begin 202001 --end 202609
sec macro cn npmi --begin 202607 --end 202609 --details
sec macro cn npmi --json
sec macro cn npmi --refresh
sec macro cn npmi --no-cache
```

`npmi` 是 `nonmanufacturing-pmi` 的别名。默认展示全国非制造业、服务业、建筑业三个商务活动指数；`--details` 额外展示九个全国分项，避免默认表格过宽。采集范围与显示方式无关，JSON 总是输出每月全部 12 项；月份参数和缓存控制与制造业命令一致。

| JSON `indicator` | 官方指标 |
| --- | --- |
| `business-activity` | 非制造业商务活动指数 |
| `services-business-activity` | 服务业商务活动指数 |
| `construction-business-activity` | 建筑业商务活动指数 |
| `new-orders` | 新订单指数 |
| `new-export-orders` | 新出口订单指数 |
| `outstanding-orders` | 在手订单指数 |
| `inventory` | 存货指数 |
| `input-prices` | 投入品价格指数 |
| `selling-prices` | 销售价格指数 |
| `employment` | 从业人员指数 |
| `supplier-delivery-times` | 供应商配送时间指数 |
| `business-expectations` | 业务活动预期指数 |

程序动态选择“采购经理指数 → 非制造业采购经理指数”目录，精确匹配全部 12 个指标后一次请求全国数据；不复用制造业的同名分项。九个分项反映非制造业全国总体，**不是服务业和建筑业各自的订单、价格或就业分项**；当前目录没有提供后者，不从新闻文本拼接或用全国值替代。

各项均保留官方季调扩散指数水平（`unit: "%"`、`seasonally_adjusted: true`），不计算“指数减 100”。50 为临界点，但解释取决于指标：投入品价格、销售价格高于 50 表示价格上升，供应商配送时间高于 50 表示交货加快，不能统一解释为经济增长加快。总指数来自官方，不使用服务业和建筑业数值的简单平均。统计说明参考[国家统计局发布](https://www.stats.gov.cn/sj/zxfb/202609/t20260930_1965449.html)。

如果某个月服务业、建筑业或其他分项缺失，该分项保持 `null`，不使用总指数补值，也不补齐缺少的整月。遇到元数据缺少必需指标、重复记录或非法数值时返回错误，避免输出貌似完整的部分结果。缓存、原始响应追溯和历史版本限制沿用上文约定。

2026-10-26 对 2020 年 1 月至 2026 年 9 月的真实查询验证得到 81 个月、972 条观测：其中 2020 年服务业、建筑业商务活动指数各缺 12 个月，共 24 条空值，其余观测有值。这是当前发布库的返回情况，不代表对应历史指标从未发布，也不承诺未来刷新仍有相同覆盖范围。

`nonmanufacturing-series.json`、`nonmanufacturing-values.json` 为 2026-10-26 获取并精简的官方元数据与 2026 年 7—9 月观测样本。测试验证 12 项映射、调查类型隔离、服务业缺失处理、零值、缓存与刷新、别名/月份校验，以及默认总览和明细输出。
