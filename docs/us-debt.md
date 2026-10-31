# 美国国债记账发行与赎回

`macro us debt` 直接查询[美国财政部Daily Treasury Statement（DTS）](https://fiscaldata.treasury.gov/datasets/daily-treasury-statement/)表IIIA的Public Debt Transactions。DTS是每日现金与债务操作报表；此处采集债务交易，不将拍卖公告或中标金额当作发行。

```bash
sec macro us debt
sec macro us debt --begin 20260901 --end 20260930
sec macro us debt --begin 20051003 --end 20051003 --json
sec macro us debt --refresh
sec macro us debt --no-cache
```

日期参数为 `YYYYMMDD`，默认UTC今天及此前6天。官方接口最早记录为2005-10-03；未发布日、周末和假日不补零。表内采用DTS原始记录日，不能由此宣称已经取得逐券CUSIP发行结算事件。跨时期表号 `III-A` 与 `IIIA` 均支持。

## 金额与分类

上游接口：

```text
https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions
```

按 `record_date`、`src_line_nbr` 升序分页，查询过滤开始／截至日期；每页1000行，上限300页。校验行数、分页总数、日期范围、跨页顺序和重复行，并核对三个金额字段的官方单位 `$1,000,000`。失败不返回部分结果，不把超出边界或分页变化当成历史缺失。

| 字段 | 定义 |
| --- | --- |
| `transaction_type` | `Issues` 发行、`Redemptions` 赎回 |
| `security_market` | `Marketable` 可流通、`Nonmarketable` 非流通 |
| `security_type`、`security_type_desc` | 官方证券类别和细项，如普通国库券、现金管理券、政府账户系列、储蓄证券等 |
| `transaction_today_amt` | 记录日金额，百万美元 |
| `transaction_mtd_amt` | 当月累计金额，百万美元 |
| `transaction_fytd_amt` | 财年累计金额，百万美元；美国财年10月开始 |

金额可能为负数，不能拒绝、截断为零或取绝对值。数据包括通胀保值证券本金调整、储蓄证券利息增量及官方更正，并非纯现金流。月累计和财年累计只保留官方值，不按天累加这些累计字段。`null`／空值与真实零值区分。

## 终端计算汇总

终端按记录日和可流通性分别展示发行、赎回及净记账发行，单位百万美元。`≈` 和JSON的 `is_derived=true` 标记计算值。发行、赎回分别为同日同市场对应明细的 `transaction_today_amt` 之和；净记账发行等于上述发行减赎回。`input_rows` 记录该组输入行数；内容完全相同的上游重复行（如2007-02-06）不计入汇总，`duplicate_rows_excluded` 记录其行数，`rows` 仍保留它们。JSON `rows` 保留全部明细、官方原始字段、请求URL和抓取时间，可按日期及市场回溯。

任一对应输入为空，或对应交易类型没有记录，该侧汇总为 `null`；两侧齐全才计算净额。全区间没有记录时明确提示，不生成连续日历零值。汇总不混加可流通与非流通，也不混加汇总行与明细；上游出现未知市场、交易类别或合计行时返回错误，待核验结构。

例：本轮2026-09-30的可流通记账发行合计232391、赎回174673，计算净额57718；该日普通国库券发行字段为-10，通胀调整为-9，按原值保留。这些计算不能解读为拍卖募集额、银行体系吸收额、公众净融资或财政赤字。

## 版本与缓存

`data_version=current-visible-revision` 仅表示当前可见历史。`published_at` 和 `revision_id` 为 `null`，不以抓取时间冒充官方公布时间或历史版本。各页原始响应以二进制安全的JSON字节字段缓存在 `~/.sec/cache/fiscaldata`，有效期六小时，URL和查询参数参与缓存键。缓存命中重新校验，失败刷新不覆盖旧快照；过期缓存不作为请求失败的回退。`--refresh` 重新获取，`--no-cache` 不读写缓存。

真实离线样本覆盖2005-10-03、2026-09-30和周末空结果，压缩为单行。回归测试覆盖负调整、空值与零、历史表号、金额单位、分页截断／重复／顺序／总数变化、错误分类、失败刷新与取消，以及CLI参数、JSON和终端单位。

## 后续范围

逐券CUSIP官方发行日期、拍卖分配、SOMA及再发行通过独立的 [`macro us auctions`](us-auctions.md) 提供，不与DTS日汇总拼接。官方现金发行／赎回总额与现金口径调整明细通过 [`macro us debt-cash`](us-debt-cash.md) 提供。逐券实际到账／结算确认和持有人净融资拆分仍需核验；DTS记账日值、官方发行日期、拍卖面值和整体现金流不能直接互换。
