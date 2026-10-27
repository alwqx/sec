# 中债国债收益率曲线

`provider/chinabond` 采集中央国债登记结算有限责任公司（China Central Depository & Clearing Co., Ltd.，CCDC，简称中央结算公司）的中债国债收益率曲线。

本实现恢复 [PR #66](https://github.com/alwqx/sec/pull/66) 删除的 `GetChinaBond`、`GetChinaBondReq`、`GetChinaBondResp` 和 `ChinaBondItem` 请求/响应结构，以及 `historyQuery` GET采集方式。代码按当前来源分包放在 `provider/chinabond`，已有 `provider/bond` 继续提供美国财政部数据。

## 查询方式

```sh
# 默认截至北京时间今天，向前共30个自然日
sec macro cn treasury

# 起止日期包含端点；最早支持20060301
sec macro cn treasury --begin 20250101 --end 20251231
sec macro cn treasury --begin 20261019 --end 20261026 --json

# 强制重新采集；或完全跳过磁盘缓存
sec macro cn treasury --refresh
sec macro cn treasury --no-cache
```

日期参数为 `YYYYMMDD`，表格显示 `YYYY-MM-DD`。仅指定 `--end` 时，默认开始日为截至日前29天，且不早于20060301；仅指定 `--begin` 时，截至日为北京时间今天。不接受未来截至日。

## 恢复的采集接口

使用PR #66中的官方接口：

```text
GET https://yield.chinabond.com.cn/cbweb-czb-web/czb/historyQuery
    ?startDate=2026-10-19&endDate=2026-10-26&gjqx=0&locale=cn_ZH&qxmc=1
```

`startDate`、`endDate` 是 `YYYY-MM-DD`；`qxmc=1` 选择中债国债收益率曲线，`gjqx=0` 返回全部关键期限。恢复原请求的JSON Accept、Content-Type、中文语言偏好和Origin头，并适配当前公共HTTP函数的超时参数。

`GetChinaBond(ctx, *GetChinaBondReq) (*GetChinaBondResp, error)` 可直接采集一个小于一年的区间。响应 `flag="0"` 表示成功，`heList` 按日期行返回；每行包含 `workTime`、`threeMonth`、`sixMonth`、`oneYear`、`twoYear`、`threeYear`、`fiveYear`、`sevenYear`、`tenYear`、`fifteenYear`、`twentyYear`、`thirtyYear`、`qxmc`。收益率为字符串，未提供的字段可能是空字符串或null，解析为Go字符串后统一为空字符串。原始响应字节仍完整保存在缓存中。

接口直接返回所属日期，无需时间戳转换或多期限序列拼接。实测数值保留两位小数，表格按两位小数显示，不补四位精度。人民币收益率单位为百分数，`1.18` 表示 `1.18%`。

2026-10-27实测接口中的 `qxmc` 字段为问号字符串；不擅自改写源字段，也不以该字段作为严格名称校验。顶层曲线名称由请求 `qxmc=1` 确定，`curve_id` 为接口选择值 `"1"`。本功能不再依赖 `queryYz` 接口、长曲线标识或毫秒时间戳。

## 宏观命令输出

保留 `Treasury(ctx, *TreasuryRequest) (*TreasuryResult, error)` 指针接口。请求为空或失败时返回 `nil, err`。顶层含地区、币种、指标、曲线名称/类型、单位、频率、请求范围与覆盖起点；`observations` 按日期升序。

| 字段 | 含义 |
| --- | --- |
| `date` | 所属日期，`YYYYMMDD` |
| `yields` | 1、2、5、10年期（`1y`、`2y`、`5y`、`10y`）的百分数值或null |
| `missing_tenors` | 上述四个期限中缺失的期限；完整时为 `[]` |
| `status` | `complete` 或 `missing-tenors` |
| `original` | `ChinaBondItem`全部源字段，保留其他期限及源名称；源null/空字符串统一为空字符串 |
| `source` | 含查询参数的官方GET接口地址 |
| `fetched_at` | 原始响应抓取时间（UTC），命中缓存保留原时间 |

空字符串、null或未提供的关键期限转为缺失值，真实零值保持为0；表格缺失显示 `—`。若整天无记录，不创建该日记录，也不自动判定休市。接口未提供逐日实际公布时间和历史修订版本，因此 `published_at` 为null；历史查询为抓取时可见版本。

HTTP错误、非JSON、失败flag、缺少heList、空数据行、非法关键期限数值、无效/重复/越界日期均报错；跨年任一年失败则整次查询失败。未知整天缺口与非交易日仍无法由该接口区分。

## 批量采集与缓存

宏观命令按自然年分批，每批不超过一年，四个关键期限和其他原始期限一次返回。2006年从3月1日起，当前年截至北京时间今天；取回后按用户请求范围过滤。已实测2006年起点周及2025年全年；2025年返回248个日期。

原始JSON、查询地址与抓取时间原子写入 `~/.sec/cache/chinabond/treasury-YYYY.json`，有效期6小时。同年不同日期区间复用缓存；北京时间跨日后当前年截止日变化，会重新获取。接口地址与旧版不同，旧缓存自动失效。`--refresh` 忽略缓存，`--no-cache` 不读写缓存。损坏或过期缓存重新采集，失败不覆盖已有缓存，也不静默回退旧数据。

## 验证样本

`provider/chinabond/testdata/` 两个JSON文件是2026-10-27保存的 `historyQuery` 官方原始响应，参数均为 `gjqx=0&locale=cn_ZH&qxmc=1`：

- `treasury-20060301-20060307.json`：2006-03-01至2006-03-07，5个所属日期。
- `treasury-20261019-20261026.json`：2026-10-19至2026-10-26，6个所属日期。

测试同时恢复PR #66的2023-06-01至2023-06-02请求参数验证，覆盖GET请求头、flag/字段校验、字符串收益率、空值与零值、日期排序及去重、缓存/刷新/取消、跨年失败，以及CLI表格与JSON。
