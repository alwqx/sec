# 宏观数据统一导出

`--standard-json` 提供版本化的宏观序列 JSON，第一批接入 CN M1/M2 与 US M2。采集仍使用原来的人民银行、FRED provider 和缓存，不增加网络请求。

```bash
sec macro cn money --begin 202412 --end 202502 --standard-json
sec macro us money --begin 202501 --end 202609 --standard-json
```

两个命令分别输出独立数据集。原有表格、别名、日期默认值、`--json`、`--refresh` 和 `--no-cache` 行为不变；`--json` 与 `--standard-json` 互斥。统一字段用于程序读取，不改变币种、单位或统计口径，也不自动生成中美对比。

## 契约版本 1

顶层包含 `schema_version="1"`、`region`、`begin/end`（YYYYMM）、`series`、`observations` 和 `original`。每个观测只包含一个值，通过 `series_id` 关联序列。消费者应检查版本，并用序列 ID 和所属期作为联合键。

`series` 为实际返回观测所引用的序列定义，包含：

| 字段 | 含义 |
| --- | --- |
| `id`、`indicator` | 序列标识、指标，例如 `us.m2.level`、`m2` |
| `measure` | `level`、`yoy_percent` 或 `mom_percent` |
| `unit` | `CNY_100_million`（亿元人民币）、`USD_billion`（十亿美元）、`percent` |
| `frequency` | 本批均为 `monthly` |
| `period_basis` | CN 为 `month-end`；US 为 `monthly-average` |
| `seasonal_adjustment` | CN 为 `not-seasonally-adjusted`；US 为 `seasonally-adjusted` |
| `price_basis` | `nominal`，余额及其增速均基于名义货币存量 |
| `annualized` | 本批均为 false；水平与增速各自标记 |
| `value_origin` | `official` 或 `derived`，不以计算值冒充官方公布值 |
| `definition`、`formula` | 统计口径标识；计算增速保留公式，官方值公式为 null |

CN 官方同比直接保留，不用展示余额重新计算。M1 在 2025 年 1 月处分成 `cn.m1.original.*` 与 `cn.m1.revised-2025.*`；M2 为 `cn.m2.pbc-m2.*`。M1 修订前后不共用序列 ID，不计算跨口径变化。US 使用 `us.m2.level`、`us.m2.yoy_percent`、`us.m2.mom_percent`，定义为 `M2SL`；同比和月环比为计算值。

## 缺失与溯源

每条观测保留 `period`（YYYYMM）、`value`、`status`、`missing_reason`、`raw_value`、`source`、`fetched_at`、`published_date`、`published_at`、`revision_id`、`data_version`、`base`、`note`。

- 数值存在时 `status="available"`，包括零值和负增速；`missing_reason=null`。
- 数值缺失时 `value=null`、`status="missing"`。`report_not_found` 表示人民银行当前目录未检索到月份；`source_value_missing` 表示源水平或报告字段缺失；计算增速另外区分 `current_value_missing`、`baseline_missing`、`zero_baseline`，其他无法计算的情况为 `calculation_unavailable`。缺失不自动归因于未发布、停更或历史不存在。
- US 计算值的 `base` 包含严格上一月／上年同月的所属期、水平和原始字符串，来自同份快照；水平和 CN 官方同比的 `base=null`。不重新计算、不跨缺口寻找替代基期。
- US 官方水平的 `raw_value` 保留 CSV 原始字符串，包括缺失标记；计算增速的原始字符串为 null。CN provider 未提供逐字段原始数字字符串，该字段为 null，不把格式化后的数值冒充原值。
- `source` 和 `fetched_at` 保留 provider 实际来源与抓取时刻，命中缓存时保留原抓取时间。未找到 CN 报告时两者均为 null，不输出公元 1 年的虚假时间。
- CN `published_date` 保留报告原有发布日期字段，仅表示源日期及其原精度；不填充零点作为精确公布时间。无法取得的 `published_at`、`revision_id` 为 null。
- `data_version="current_visible_revision"` 表示查询时当前可见历史；不能据此进行“当时已知”的历史回测，也不提供完整 vintage 档案。US 原 provider 的版本标签另外保存在 `original` 中。

`original` 是适配前的完整 provider JSON 结果，保留原统计注释、方法变化、基期和来源字段。它**不是上游 HTTP 原始响应**；原始 HTML/CSV 的缓存策略仍由 provider 管理。新增适配不会改写原 provider 结果。

网络、解析、编码失败直接报错，不输出部分成功的数据集；状态字段不用于把请求失败伪装为数据缺失。当前不增加数据过期判定，沿用 provider 六小时缓存与失败处理；`available` 仅表示该观测有数值，不承诺它是最新公布期。

## 覆盖与后续迁移

本批完成货币指标的统一元数据、口径、溯源和缺失状态试点，未将所有宏观 provider 迁移。其余指标继续使用原 JSON，TODO 中全局基础项仍保留未完成状态。日度、季度、累计值、1—2 月合并发布、部分抓取失败、完整历史版本与中美可比性需要各自的适配和测试，不能直接套用本批月度货币序列。

测试覆盖真实离线样本、旧输出回归、官方值／计算值、M1口径边界、零与缺失、严格基期、空结果、元数据空值、重复月份、非法范围、非有限值、上游错误、取消及输出写入失败。
