# BLS：美国劳工统计局

BLS（Bureau of Labor Statistics）是美国劳工统计局，`provider/bls` 使用其官方 Public Data API。当前实现美国CPI-U（所有城市消费者价格指数）的美国城市平均总指数及核心指数，独立于CN数据；核心指数剔除食品和能源。

## 使用方式

```sh
# 默认截至UTC上个月，共12个月
sec macro us cpi
sec macro us core-cpi

sec macro us cpi --begin 202501 --end 202609
sec macro us core-cpi --begin 202501 --end 202609 --json
sec macro us cpi --refresh
sec macro us cpi --no-cache
```

起止月份为 `YYYYMM`，包含端点；表格显示 `YYYY-MM`。仅指定截至月时默认向前共12个月，最早不超过191301；仅指定开始月时截至UTC上个月。拒绝无效月份、逆序范围及晚于当前UTC月份的截至月。当前月或上个月可能尚未发布，保留缺失而不估算。

## 官方接口和系列

使用无需注册/密钥的 `POST https://api.bls.gov/publicAPI/v1/timeseries/data/`，请求体包含四个 `seriesid` 以及字符串 `startyear`、`endyear`。依照[BLS v1接口说明](https://www.bls.gov/developers/api_signature.htm)，单批最多十个自然年，历史范围较长时拆批；不让上游静默缩短年份。无需注册接口的每日请求额度有限，[BLS FAQ](https://www.bls.gov/developers/api_faqs.htm)说明未注册用户每日最多25次查询。

| 指标 | 非季调（NSA） | 季调（SA） |
| --- | --- | --- |
| CPI-U总指数 | `CUUR0000SA0` | `CUSR0000SA0` |
| 核心CPI-U（剔除食品和能源） | `CUUR0000SA0L1E` | `CUSR0000SA0L1E` |

上述均为美国城市平均、月度指数，基期为1982—1984=100。[BLS系列编码说明](https://www.bls.gov/cpi/factsheets/cpi-series-ids.htm)解释了消费者类别、季调标识、地区和项目编码；核心指标代码亦可见[BLS研究中的指标表](https://www.bls.gov/opub/mlr/2024/article/assessing-and-improving-the-accuracy-of-the-cpi.htm)。本功能不混入CPI-W、链式CPI、特定城市或仅剔除食品的指数。

每批同时取得四条序列，两个命令在请求年份相同时复用同一缓存。为计算首月增速，会额外取得前一年数据；取回后仅展示请求月份，计算基期则在JSON中单独保留。每批不超过十个自然年，例如需要2016—2026年时拆为2016—2025与2026两个批次。月度点使用 `M01`—`M12`；`M13`年平均不作为月份，也不参与增速计算。

## 指数和增速口径

- 表格并列展示非季调和季调原始指数，保留三位小数。
- 同比（%）=（当月NSA指数 / 上年同月NSA指数 − 1）×100。
- 月环比（%）=（当月SA指数 / 上月SA指数 − 1）×100，未年化。
- 增速为基于返回指数的计算值，表格保留两位小数，JSON保留计算精度；不将其冒充BLS单独发布的四舍五入增速。
- 仅使用严格对应的上年同月或上月；缺少当月值、基期值或基期为零时，增速为null，不跨缺口寻找替代月份，也不将两月变化称为单月环比。

各序列起点不同。支持从191301查询，不代表当时四条序列均存在；尚未起始的系列保留缺失。2026-10-27实测1913—1914年：CPI总指数NSA有24个月，其他三条序列返回空数组并带“No Data Available”说明。近期样本包含2024年1月至2026年9月，不承诺所有历史月份均连续。

## 缺失数据、脚注和版本

请求范围内每个月均输出一项。源值 `"-"` 或空字符串转为null，原始文本仍保留；完全未返回的月份对应值对象为null。真实零值保留为0，零基期不执行除法。2025年10月在实测响应中为 `"-"`，附有 `X` 脚注，解释为2025年拨款中断导致数据不可用；该缺口保留，2025年11月的月环比也因基期缺失而为null。

响应必须成功并含全部四条系列（其中个别系列可为空数组）。配额失败、HTTP错误、缺少/重复/未知系列、非法/重复/越界月份、非法指数，以及提示年份被缩短等消息均报错。仅正常保留“No Data Available for Series ...”消息。跨批次任何一批失败，整次命令报错，不输出部分历史。

[BLS季调说明](https://www.bls.gov/cpi/seasonal-adjustment/)指出，年度季调因子更新可能修订最近五年的季调指数。本接口提供当前可见数据，并非按当时信息重建的历史版本；`data_version="latest-available"`，实际逐月公布时间不可得，`published_at=null`。`fetched_at`仅代表抓取时间。长范围结果保留每个批次、当前值及基期值各自的抓取时间，不承诺批次间为同一原子快照；需要核验新发布或年度修订时使用 `--refresh`。

## JSON字段

顶层保留地区US、指标、调查人群、频率、指数基期、请求范围、官方接口来源、版本说明，以及两条所选系列的ID和季调状态。`batches`包含每批起止年、官方消息与UTC抓取时间；请求固定四条系列，参数可据此重现。

每条 `observations` 包含：

| 字段 | 含义 |
| --- | --- |
| `period` | 所属月份，`YYYYMM` |
| `not_seasonally_adjusted` | 当月NSA值对象或null |
| `seasonally_adjusted` | 当月SA值对象或null |
| `yoy_percent` / `mom_percent` | 计算同比 / 季调月环比，单位%或null |
| `yoy_base` / `mom_base` | 同比 / 环比所用严格基期值对象，保留其缺失状态和来源ID |
| `note` | 中文缺失值/基期说明 |

值对象含 `period`、`series_id`、数值 `index`、源字符串 `raw_value`、有效 `footnotes`、官方 `latest` 标识及 `fetched_at`。空脚注对象 `{}` 去除；非空脚注代码与原文保留。`latest`是API在该次响应上的标识，不是该历史月份的首次公布时间。

## 缓存和验证

原始成功响应与抓取时间原子写入 `~/.sec/cache/bls/`，有效期六小时；键包含官方接口地址、四条系列和起止年份。`--refresh`跳过读取并更新缓存，`--no-cache`不读写磁盘。只缓存经过系列和数据校验的响应，损坏、过期或结构异常缓存重新请求，错误不会覆盖已有正常缓存，也不悄悄回退过期值。

本地测试全部使用HTTP模拟。`provider/bls/testdata/cpi-2024-2026.json`、`cpi-1913-1914.json` 为2026-10-27从官方v1接口保存的原始响应，四条系列如上，分别请求2024—2026年和1913—1914年。覆盖季调/非季调及核心口径、真实缺失脚注、不跨缺口、年平均过滤、十年分批边界、缓存/刷新/取消/恢复、配额/部分失败及命令输出；不会因运行测试消耗BLS查询额度。
