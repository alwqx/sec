# 中国服务贸易月度统计

`sec macro cn services-trade` 采集[商务部服务进出口月度统计](https://data.mofcom.gov.cn/fwmy/monthly.shtml)，与 `macro cn trade` 的海关货物贸易独立查询。采集实现位于 `provider/mofcom/services.go`，复用商务部响应缓存。

## 命令

```bash
sec macro cn services-trade
sec macro cn services-trade --begin 202501 --end 202608
sec macro cn services-trade --begin 201601 --end 202608 --json
sec macro cn services-trade --refresh
sec macro cn services-trade --no-cache
```

月份参数为 `YYYYMM`，终端显示 `YYYY-MM`。默认查询北京时间上个月及此前11个月，最早支持201601。本轮从官方接口核验到的最早记录为2016年1月；这不是无缺口历史的承诺。请求范围内没有记录的月份仍输出，`cny`、`usd`、`fetched_at` 为 `null`，并注明缺失。实际目录已见2026年1、2月等缺口，不拆分合并期，也不用相邻累计金额推算单月值。

## 字段与口径

原网页按人民币／美元切换展示，接口一次返回两组原始值。金额为现价月度值，未作季调、未折年。不是海关货物进出口，也不是服务外包合同或执行金额。

| JSON字段 | 人民币源字段 | 美元源字段 | 定义 |
| --- | --- | --- | --- |
| `total` | `importAndExportRMB` | `importAndExportUSD` | 当月服务进出口总额 |
| `exports` | `exportRMB` | `exportUSD` | 当月出口 |
| `imports` | `importRMB` | `importUSD` | 当月进口，按上游正金额保留 |
| `balance` | `differenceRMB` | `differenceUSD` | 官方差额，正为顺差、负为逆差 |

人民币单位为**亿元人民币**，美元单位为**亿美元**。两个币种均直接采集，不以汇率互换。不用出口加减进口覆盖官方总额或差额：例如本轮2026年8月美元出口361.4、进口579.8，官方差额是-218.5，存在独立舍入。缺失值与真实零值区分；JSON保留总额及原始字段，终端显示出口、进口、差额。

本命令记录 `region`、`indicator`、频率、月度／现价口径及单位。每条源记录保留真实请求URL、抓取时间、完整原始字段；`published_at`、`revision_id` 为 `null`。上游 `createTime`、`updateTime` 及字符串形式是网站记录时间，不用作统计所属期、官方首次公布时间或完整修订档案。所属期只取 `mouth` 字段，历史标为 `current-visible-revision`。

`first_period`、`last_period` 是**所请求区间内实际存在记录**的起止月，空查询时为空字符串，不代表整个数据源的永久覆盖范围。源记录列出的单个空值保留在对应币种字段中；整月未列示时，该月币种对象为空。

伙伴和行业分项没有在该月度总量接口中提供，保留后续TODO，不以货物贸易伙伴代替服务贸易伙伴。

## 请求、分页和缓存

接口与官方网页参数一致：

```text
GET https://data.mofcom.gov.cn/datamofcom/front/fwmy/mouthly?start_date=202607&end_date=202608&pageNumber=1
```

`mouthly` 和 `mouth` 是上游的原始命名。逐页校验 `total`、`pageSize`、`pageNumber`、`maxPageNum` 和行数，最多300页；同时检查月份顺序、重复和越界，分页间总数／页面大小变化或网络失败直接报错，不返回不完整结果。

原始响应缓存在 `~/.sec/cache/mofcom`，有效期六小时，缓存键包含请求URL及其参数。`--refresh` 跳过旧缓存，只有通过校验的响应才写入；失败刷新不覆盖旧缓存。`--no-cache` 不读写缓存。缓存命中也重新做语义校验，未来时间戳及损坏响应不会复用。

## 测试

`provider/mofcom/testdata/services-monthly.json` 为本轮从官方接口取得的2026年7—8月精简样本，保留两条原始记录，分页计数按样本大小重设。Go回归测试覆盖人民币／美元单位、官方差额独立舍入、零与缺失、未发布月份、字段缺失及非法数值、分页截断／重复／总数变化、缓存命中、失败刷新、取消、非法日期、命令JSON和终端输出。
