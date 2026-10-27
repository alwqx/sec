# FRED：美国官方经济统计转接

FRED（Federal Reserve Economic Data）由圣路易斯联邦储备银行提供。`provider/fred` 当前采集BEA（Bureau of Economic Analysis，美国经济分析局）编制、经FRED转接的PCE价格指数。FRED是分发来源，BEA是原始编制机构，JSON分别保留二者。

## 使用方式

```sh
sec macro us pce
sec macro us core-pce
sec macro us pce --begin 202501 --end 202609
sec macro us core-pce --begin 202501 --end 202609 --json
sec macro us pce --refresh
sec macro us pce --no-cache
```

起止月份为 `YYYYMM`，包含端点。默认截至UTC上个月，共12个月；仅指定截至月时默认向前11个月，不早于195901。拒绝逆序、无效月份、早于195901和晚于当前UTC月的范围。未发布或缺失月份返回null，表格显示 `—`，不补值。

## 官方序列及价格口径

| 命令 | FRED系列 | BEA账户代码 | 定义 |
| --- | --- | --- | --- |
| `pce` | [PCEPI](https://fred.stlouisfed.org/series/PCEPI) | DPCERG | 个人消费支出链式价格指数 |
| `core-pce` | [PCEPILFE](https://fred.stlouisfed.org/series/PCEPILFE) | DPCCRG | 剔除食品和能源的个人消费支出链式价格指数 |

两条序列的官方定义均为**月度、季调、2017=100的链式价格指数**。原始发布是BEA的Personal Income and Outlays；[BEA价格指数说明](https://www.bea.gov/data/personal-consumption-expenditures-price-index)介绍了该指数衡量的消费价格变化。它们不是以美元计价的PCE消费金额，也不是实际消费量或链式数量指数；后两类继续作为独立TODO。

当前采集不依赖BEA或FRED API密钥，使用FRED公开CSV下载：

```text
GET https://fred.stlouisfed.org/graph/fredgraph.csv?id=PCEPI,PCEPILFE
```

不附带单位变换、频率聚合或增长率转换参数。一次获取两条系列的完整历史，再按请求月份筛选；因此每条记录及同比/月环比基期来自同一份CSV快照，两个命令、不同范围可共用缓存。2026-10-27实测历史自1959年1月至2026年8月，共812个月，CSV仅约20KB，无需按月或按年反复请求。FRED公开下载端点对默认sec/Go User-Agent会中断连接，采集器使用其接受的通用下载客户端标识；不依赖本机curl程序。

## 增速计算与缺失

- 同比（%）=（当月季调价格指数 / 上年同月季调价格指数 − 1）×100。
- 月环比（%）=（当月季调价格指数 / 上月季调价格指数 − 1）×100，未年化。
- 两项均为同一季调序列的计算值；与CPI命令采用NSA同比、SA环比的组合有区别，JSON保留季调标记。
- 计算基期严格为对应日历月份；当月值或基期缺失、基期为零时返回null，不跨缺口寻找替代值。
- 保留源字符串和数值；源空单元格、`.`表示缺失，真实0仍为0。表格指数显示三位小数、计算增速显示两位，JSON保留计算精度；不冒充BEA另行发布并四舍五入的增速。

CSV按列名选择PCEPI和PCEPILFE，支持列顺序变化及 `observation_date` / `DATE` 日期列。未知/重复列、非月初日期、重复月份、损坏CSV、空历史、负值/非有限数等异常会报错，避免将其他指标或频率误当作价格指数。HTTP或解析失败时整次查询失败，不静默返回部分数据。

## JSON与版本

顶层保留地区US、指标、FRED系列ID、BEA账户代码、月度频率、价格指数类型、季调状态、2017基期、请求范围、编制/分发机构、CSV/系列/BEA来源链接，以及该快照UTC抓取时间。

每条 `observations` 含 `period`、`index`、`raw_index`、`yoy_percent`、`mom_percent`、`yoy_base`、`mom_base` 和缺失说明。两个基期对象同样包含月份、指数及源文本，即使未返回基期数据也保留应使用的月份。

BEA可能修订历史值及调整价格指数参考年。结果标记 `data_version="latest-available"`，仅代表抓取时可见版本；CSV没有逐月实际公布时间、修订编号或历史版本档案，所以 `published_at`、`revision_id` 为null，不把抓取时间当作公布时间，也不承诺重建历史初值。当前2017参考年按上述FRED官方系列元数据核验，未从CSV数值推断。需要当时可见版本的回测仍需独立的版本档案采集。

## 缓存与验证

原始CSV、来源地址和抓取时间原子写入 `~/.sec/cache/fred/pce-prices.json`，有效期六小时。两个指标和不同月份范围复用同一快照。`--refresh`跳过读取并更新缓存；`--no-cache`不读写磁盘；过期、来源不符或无效CSV缓存会重新采集。失败不覆盖已有有效缓存，不回退过期值。

`provider/fred/testdata/pce-prices.csv` 为2026-10-27下载的官方完整快照。测试使用模拟HTTP，不依赖在线数据；覆盖两个价格系列及BEA代码、真实历史/增速、基期与未发布月份、缺口不跨越、空值/零值、列重排、非法响应、缓存/刷新/不缓存/恢复、取消请求，以及命令表格/JSON。实测命令与官方系列页面的最新月值一致。
