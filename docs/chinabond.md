# 中债国债收益率曲线

`provider/chinabond` 恢复 [PR #66](https://github.com/alwqx/sec/pull/66) 删除的 `GetChinaBond`、`GetChinaBondReq`、`GetChinaBondResp` 和 `ChinaBondItem`，通过中债官方 `historyQuery` GET接口采集中国国债收益率。已有 `provider/bond` 继续提供美国财政部数据。

## 查询方式

```sh
# 默认截至北京时间今天，向前共30个自然日
sec macro cn treasury

# 按指定范围直接请求；单次区间小于一年
sec macro cn treasury --begin 20250101 --end 20251231
sec macro cn treasury --begin 20261019 --end 20261026 --json
```

CLI起止日期为 `YYYYMMDD`，包含端点，最早支持20060301。仅指定 `--end` 时默认向前共30个自然日，且不早于覆盖起点；仅指定 `--begin` 时截至北京时间今天。不接受未来截至日或逆序日期。

命令直接调用 `chinabond.GetChinaBond(ctx, *GetChinaBondReq)`，将日期转换为 `YYYY-MM-DD` 后按指定范围请求，不扩展到全年。已删除原 `Client.Treasury` 包装、年度缓存和相应的 `--refresh`、`--no-cache` 参数。

## 官方接口与字段

```text
GET https://yield.chinabond.com.cn/cbweb-czb-web/czb/historyQuery
    ?startDate=2026-10-19&endDate=2026-10-26&gjqx=0&locale=cn_ZH&qxmc=1
```

`qxmc=1` 选择中债国债收益率曲线，`gjqx=0` 返回全部关键期限。保留原实现的JSON请求头、中文语言偏好和Origin，适配当前HTTP公共函数的30秒超时及响应状态检查。

`GetChinaBond(ctx, *GetChinaBondReq) (*GetChinaBondResp, error)` 接受起止日期字符串 `YYYY-MM-DD`，单次区间小于一年。请求为空、失败或数据无效时返回 `nil, err`。

`--json` 直接输出类型化的接口响应，不再封装额外的宏观元数据：

- `flag`：成功值为字符串 `"0"`。
- `heList`：保留接口顺序（实测日期倒序），每项包含 `workTime`、`threeMonth`、`sixMonth`、`oneYear`、`twoYear`、`threeYear`、`fiveYear`、`sevenYear`、`tenYear`、`fifteenYear`、`twentyYear`、`thirtyYear`、`qxmc`。
- 收益率字段保持字符串；源null、空字符串或未提供的字段经Go字符串结构解析后均为空字符串。真实 `"0"` 保留为零，不作缺失值处理。

接口直接提供所属日期，不转换时间戳。人民币收益率单位为百分数，`"1.18"` 表示 `1.18%`，源值实测保留两位小数。表格按日期升序显示3个月、6个月和1、2、5、10年期，保留源数值文本，缺失显示 `—` 并标注“期限缺失”。JSON还包含其他期限。

2026-10-27实测接口中的 `qxmc` 名称为问号字符串，保留该源值，不擅自改写。所选曲线由请求参数 `qxmc=1` 确定。

## 数据校验与限制

校验HTTP状态、响应flag与heList、非空数据行、有效且不重复/越界的日期，以及六个展示期限的有限数值。空数据数组仍为有效响应，不代表收益率为零；整天无记录不自动判为休市，也不补值。

接口未提供逐日实际公布时间、抓取时间字段或历史修订版本，JSON不额外添加这些信息；历史查询是当前可见版本。单次查询过长时直接报错，不隐式拆分或返回部分数据。每次命令执行直接请求官方接口，不读写本地缓存。

## 验证

`provider/chinabond/testdata/` 两个JSON文件为2026-10-27保存的官方原始响应，固定参数均为 `gjqx=0&locale=cn_ZH&qxmc=1`：

- `treasury-20060301-20060307.json`：2006-03-01至2006-03-07，5个日期。
- `treasury-20261019-20261026.json`：2026-10-19至2026-10-26，6个日期。

测试验证PR #66的请求参数和GET请求头、真实响应字段、空值与零值、flag错误、异常数值/日期、重复日期及表格/JSON输出。CLI测试严格匹配用户给定的截至日，确保不再按全年请求。
