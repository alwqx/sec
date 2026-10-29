# 美国住宅销售与房价指数

住宅销售数量与房价指数使用不同频率和定义，分别提供 `macro us housing` 和 `macro us house-price`。数据来自Census/HUD、NAR和FHFA，经FRED公开CSV转接，不使用售价中位数冒充房价指数，不与中国数据或其他资产拼接。

## 命令

```bash
sec macro us housing
sec macro us housing --component new-sales --begin 202501 --end 202609
sec macro us housing --component existing-sales --begin 202509 --end 202609 --json
sec macro us housing --refresh
sec macro us house-price
sec macro us house-price --begin 2024Q1 --end 2026Q2 --json
sec macro us house-price --no-cache
```

销售默认 `--component all`，分别展示新房和成屋，不加总。月份使用 `YYYYMM`，默认UTC上个月及此前11个月；表格格式化成 `YYYY-MM`。房价季度使用 `YYYYQ1` 至 `YYYYQ4`，默认UTC上一个完整季度及此前7季，最早1975Q1；数据尚未发布时保留空值，不把默认截止季度当成已发布季度。`--json` 输出来源和完整口径，`--refresh` 跳过现有缓存并更新，`--no-cache` 不读写磁盘缓存。

## 系列与单位

| 命令分类 | FRED系列 | 原始频率和单位 | 季调/折年 | 本次公开快照有效范围 |
| --- | --- | --- | --- | --- |
| `housing --component new-sales` | `HSN1F` | 月度，千套 | 季调折年率 SAAR | 1963-01至2026-09 |
| `housing --component existing-sales` | `EXHOSLUSM495S` | 月度，套 | 季调折年率 SAAR | 2025-09至2026-09，仅13个月 |
| `house-price` | `USSTHPI` | 季度，指数，1980Q1=100 | 未季调 NSA、不折年 | 1975Q1至2026Q2 |

表内范围来自2026-10-28下载的实际CSV；运行时的 `first_period`、`last_period` 按各系列的非空值动态计算，不将成屋窗口起点硬编码为永久系列起点。每次下载只代表当前公开快照，历史数据可能修订、可见范围可能改变。成屋早期历史仍是后续数据源覆盖工作，不宣称本次已取得完整成屋历史。

**新房千套与成屋套相差1000倍。** 例如2026年9月原始值分别为676千套和3990000套，均为季调折年水平；不能直接拿676与3990000比较，也不能把它们当成当月实际成交量。代码保留原始单位，不擅自除以12转换成当月真实套数。两者成交阶段和住宅类型也不同，不提供加总。

**新房销售**来自Census/HUD的新建待售单户住宅调查。签订销售合同或收取定金即计为销售，可以在开工前、施工中或竣工后发生；调查不继续追踪最终成交交割。它不是全部新建住房竣工数，也不等于新房交割数。

**成屋销售**来自NAR的既有住宅成交统计，包含既有单户住宅、共管公寓和合作公寓，采用地方协会和MLS的代表性样本。它统计已完成的住宅交易，与新房签约/收取定金的阶段不同。当前公开FRED CSV仅提供13个月有效值；较早月份即使存在日期行，成屋列也为空。查询旧年份时返回明确缺失和覆盖说明，不通过旧值填补、第三方拼接或最近观测回退制造完整历史。

**FHFA全交易房价指数**是Fannie Mae和Freddie Mac购入或证券化的常规合规贷款所对应单户住宅的重复交易指数；在购房样本基础上加入再融资评估值。它不是仅购房的Purchase-Only指数，也不涵盖所有现金、非合规贷款或所有住宅交易。`USSTHPI`采用1980Q1=100的基期并且未季调，不能以售价中位数、售价均值或其他基期指数替代。

## 增速、缺失与修订

- 同比为 `(当期/严格上年同期 - 1) × 100`，月度使用上一年同月，季度使用上一年同季。
- 上期变化为 `(当期/严格上期 - 1) × 100`。销售是月环比，房价是季环比，均不年化。
- 房价指数本身未季调，表头和说明明确标注“未季调季环比”，不能将此季环比解释成剔除季节效应后的房价趋势。
- 基数可位于查询窗口之外，但必须来自同一份当前快照。严格日历基期缺失或为零时增速为 `null`，不跨期使用最近有效数据。
- 源数值缺失与真实零值区分，JSON保留原始字符串和 `null`，表格显示 `—`；销售量真实零值可保留，房价指数非正值视为错误。代码不插值，不推算尚未发布的数据。
- 销售季调数据和FHFA历史指数会修订；结果标记 `data_version=current_visible_revision`，没有历史初值或完整vintage承诺。`fetched_at`为下载时间，`published_at`及`revision_id`明确为 `null`，不会用抓取时间冒充逐期官方公布时间。

JSON的每条 `series` 保留原始单位、基期、季调/折年标记、机构、系列URL、官方来源、版权归属和动态有效覆盖；`observations` 中保留数值原文、分类、系列ID、单位、同比、上期增速以及 `previous_period_base` / `yoy_base` 的严格期间和原始水平。

## 公开访问与版权

核验时三个系列均可通过FRED公开CSV访问，无需API密钥。新房和FHFA系列被FRED标为 `Public Domain: Citation Requested`，使用时仍保留机构和FRED归属。成屋系列被FRED标为 `Copyrighted: Citation Required`，FRED说明版权归National Association of Realtors，FRED经许可转载；该说明不代表本项目自动获得独立的额外再分发许可。成屋表格和JSON均保留NAR归属和系列引用，文档保留原来源；公开可取不等于无限使用授权。

建议引用：Census/HUD New One Family Houses Sold `[HSN1F]`；National Association of Realtors Existing Home Sales `[EXHOSLUSM495S]`；FHFA All-Transactions House Price Index `[USSTHPI]`，均经FRED转接，并注明实际访问日期。

## 请求与缓存

公开CSV：

```text
https://fred.stlouisfed.org/graph/fredgraph.csv?id=HSN1F,EXHOSLUSM495S
https://fred.stlouisfed.org/graph/fredgraph.csv?id=USSTHPI
```

复用 `provider/fred.Client.csvSnapshot`，销售和房价分别缓存到 `~/.sec/cache/fred/us-housing-sales.json`、`us-housing-price.json`，六小时有效。销售分类共享同一完整双系列快照，切换分类和窗口可复用缓存。

先校验列名、日期和频率、重复期间、有限且合法的源值、各系列有效观测以及可计算增速，再原子写缓存。响应错列、损坏或异常刷新不会覆盖旧缓存；已有坏缓存会触发重新下载。未来时间戳、来源URL不匹配和过期缓存不能复用；下载失败直接报错，不以旧缓存伪装新结果。真实离线样本位于 `provider/fred/testdata/us-housing-sales.csv`、`us-housing-price.csv`，仅用于解析、口径、缓存和命令测试。

## 官方参考

- [Census住宅统计定义](https://www.census.gov/construction/soc/definitions.html)：New Houses Sold的签约和定金认定，及不追踪最终交割。
- [Census New Residential Sales](https://www.census.gov/construction/nrs/)
- [NAR成屋销售方法](https://www.nar.realtor/research-and-statistics/housing-statistics/existing-home-sales/methodology)
- [NAR成屋销售](https://www.nar.realtor/research-and-statistics/housing-statistics/existing-home-sales)
- [FHFA房价指数](https://www.fhfa.gov/data/hpi)
- [FHFA HPI常见问题](https://www.fhfa.gov/faqs/hpi)：All-Transactions包含再融资估值、重复交易法及样本限制。
- [FRED新房系列及单位/引用](https://fred.stlouisfed.org/series/HSN1F)
- [FRED成屋系列及单位/版权](https://fred.stlouisfed.org/series/EXHOSLUSM495S)
- [FRED房价系列及基期/NSA口径](https://fred.stlouisfed.org/series/USSTHPI)
