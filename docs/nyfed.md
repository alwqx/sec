# 纽约联储（NY Fed）有效联邦基金利率

`nyfed` 指纽约联邦储备银行（Federal Reserve Bank of New York），是 `provider/nyfed` 的官方数据来源。

## 命令

```sh
sec macro us effr
sec macro us effr --begin 20250101 --end 20250110
sec macro us effr --begin 20160225 --end 20160304 --json --refresh
sec macro us effr --no-cache
```

日期参数使用 `YYYYMMDD`，按交易所属日期筛选，含首尾。默认截至 UTC 今天，查询最近30个自然日。此直接接口最早观测为2000-07-03；更早日期直接报错。默认缓存位于 `~/.sec/cache/nyfed/`，有效期六小时；`--refresh` 绕过读取并更新成功缓存，`--no-cache` 不读写磁盘。缓存保存请求地址对应的原始响应和抓取时间，只有通过数据校验的响应才能原子替换缓存；失败不覆盖已有快照。

## 官方来源与接口核验

- [Markets API 文档](https://markets.newyorkfed.org/static/docs/markets-api.html)，其 [OpenAPI 描述](https://markets.newyorkfed.org/static/docs/markets-api.yml) 定义 `GET /api/rates/unsecured/{ratetype}/search.{format}`。
- [EFFR 官方页面](https://www.newyorkfed.org/markets/reference-rates/effr)。
- [参考利率方法、公布与修订规则](https://www.newyorkfed.org/markets/reference-rates/additional-information-about-reference-rates)。
- [2016年统计方法切换说明](https://www.newyorkfed.org/markets/effr-obfr-data)。

本实现固定请求 `https://markets.newyorkfed.org/api/rates/unsecured/effr/search.json`，使用 `startDate`、`endDate`（`yyyy-MM-dd`，均含首尾）；无需 API key。文档未列出该搜索接口的日期跨度、条数、分页或调用配额上限，因此不假定某个未公布的限制。客户端使用30秒超时和16MiB响应上限。2026-10-28实际核对2000-07-03至2025-01-06：单次请求返回6160条；按2012/2013年边界拆为两次请求，分别3143和3017条，合并后的全部日期和字段与单次请求完全一致，未发现截断。该核验不代表上游未来不会调整限制。2026-10-28核验：从2000-01-01查询到2000-07-03只返回2000-07-03一条；查询早于该所属日期的区间返回空数组。真实响应使用 `percentRate`，而 OpenAPI 中示例字段写作 `percent`；解析按真实 JSON 字段 `percentRate`，字段缺失时拒绝响应，不将其当成0。

离线样本保存在 `provider/nyfed/testdata/`，为官方原始 JSON：`effr-early.json`（2000-07-01至07-10，含 `revisionIndicator: "r"`）、`effr-methodology.json`（2016-02-25至03-04）、`effr-holidays.json`（2024-12-27至2025-01-06）。样本包含周末及元旦缺口、旧新口径字段差异；不会将缺口补为0或沿用前值。

## 日期、单位及比较

`effectiveDate` 是隔夜交易所属日期，并非公布日期。现行常规安排为下一个纽约联储营业日约美东时间09:00公布前一营业日的利率，符合条件的修订约14:30公布。节假日、临时休市和特殊情况可能改变安排。API响应没有逐条实际公布时间，所以 `published_at` 永远为 `null`，不会根据日历或抓取时间推算。`fetched_at` 仅代表该快照采集时间，读取缓存时保留原抓取时间。

`rate_percent` 的单位是百分数，例如4.33表示4.33%，不是0.0433。`change_basis_points = (本次百分数 - 上一可用观测百分数) × 100`；`previous_date` 明确给出基期所属日期。这是相邻可用观测变化，不是严格自然日日变化。请求先向前取31个自然日上下文，仍找不到前值时按一年窗口继续向前查到最早数据；计算后只输出所选区间。无前值及2016-03-01跨统计方法边界的观测变化为 `null`（跨界仍保留 `previous_date`）。不会生成未公布日期行。

## 方法、修订及原始字段

2016-03-01起，EFFR由FR2420报告中的隔夜联邦基金交易计算交易量加权中位数；此前使用经纪商数据的交易量加权均值，跨此日期比较存在口径变化。官方可能因数据不足使用备用来源，并通过脚注注明，所以行级 `methodology` 表示该时期的常规方法，特殊情况应结合原始脚注核查。

`revision_indicator` 原样保留官方标记（空串、`r`、`Y`等不自行改写含义）；`raw` 完整保留每条官方对象，含可获得的分位数、`volumeInBillions`（十亿美元）、`footnoteId`、目标区间和旧口径日内高低值等字段。保留目标区间原字段只用于追溯，此命令分析指标为实际 EFFR，目标区间采集仍属独立功能。结果以 `region: US`、`frequency: daily`、`data_version: latest-available` 标明区域、频率和快照版本；原始 `revisionIndicator` 是该条记录携带的标记，不代表完整的历史版本。当前查询得到当前可见修订值，并非历史时点可见值；此接口未提供修订前版本或实际发布时间，不适合作无前视偏差回测。
