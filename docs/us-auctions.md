# 美国国债逐券发行与拍卖结果

`sec macro us auctions` 通过美国财政部Fiscal Data公开拍卖查询接口采集可流通国债，包含国库券、普通附息债、通胀保值债及浮息债，独立于DTS记账交易。

```bash
sec macro us auctions
sec macro us auctions --begin 20260901 --end 20260930
sec macro us auctions --date-basis auction --begin 20260801 --end 20260831 --json
sec macro us auctions --begin 19980727 --end 19980831 --json
sec macro us auctions --refresh
sec macro us auctions --no-cache
```

## 日期与事件

日期使用 `YYYYMMDD`，默认UTC今天及此前29天；默认 `--date-basis issue` 按官方发行日筛选，`auction` 按拍卖日筛选。最早统一查询边界为1998-07-27。TreasuryDirect说明其他证券的公开查询自该拍卖日起可用，TIPS最早可到1997年，但本命令不为单个类型扩展统一查询边界。字段在2008年4月及2026年10月有扩充，不能承诺早期全部字段齐全。

JSON分别保留 `announcement_date`、`auction_date`、`issue_date`、`maturity_date`、`original_issue_date`。发行日不是单笔资金实际到账确认，原发行日不能用于筛选本次再发行。按拍卖日查询可能包含发行日尚未来到的记录；不补未列示日期，也不把它们当作零发行日。

同一CUSIP会出现在多个再发行事件中，事件按CUSIP、发行日、拍卖日共同区分。不会仅凭CUSIP去重。`reopening` 保留官方Yes／No，未知为 `null`。FRN和TIPS分别以 `floating_rate`、`inflation_indexed` 标记，不能仅按 `security_type` 的Note／Bond判断。

## 面值、分配和包含关系

所有 `amounts` 字段直接取官方原值，单位为**美元面值**；不是DTS的百万美元，也不是实际现金收入。终端仅为阅读将总中标和SOMA除以10亿，表头说明单位，JSON不改变原单位。

| JSON字段 | 官方字段 | 口径 |
| --- | --- | --- |
| `offering_usd` | `offering_amt` | 官方发行额 |
| `total_accepted_usd` | `total_accepted` | 总中标面值 |
| `competitive_accepted_usd` | `comp_accepted` | 竞争性中标 |
| `noncompetitive_accepted_usd` | `noncomp_accepted` | 非竞争性中标 |
| `soma_accepted_usd` | `soma_accepted` | SOMA中标 |
| `fima_noncompetitive_accepted_usd` | `fima_noncomp_accepted` | FIMA非竞争性中标 |
| `primary_dealer_accepted_usd` | `primary_dealer_accepted` | 一级交易商中标 |
| `direct_bidder_accepted_usd` | `direct_bidder_accepted` | 直接投标人中标 |
| `indirect_bidder_accepted_usd` | `indirect_bidder_accepted` | 间接投标人中标 |

各分项可能包含或交叠，不对这些字段任意加减以重建发行额、总中标或公众认购。`soma_inside_offering_amount` 来自 `soma_included`，而 `fima_competitive_inside_offering_amount` 来自 `fima_included`，后者官方标签特指FIMA**竞争性**部分，不能套给FIMA非竞争性分配。缺失标记为 `null`，不猜测Yes或No。

例：2026-09-01发行、2026-08-27拍卖的912797VE4，发行额100000000000美元，总中标108522561100美元，SOMA8521960400美元；SOMA不含在发行额内，但不能因此把SOMA再加到总中标上。面值也不能直接与价格相乘后宣称为完整现金收入，附息债应计利息和TIPS指数调整等另有口径，本次不计算。

`price_per_100` 保留每100美元面值的价格。利率、收益率、贴现率、利差、发行规模、原始日期及官方结果文件名完整保存在 `raw_values`，不将不同证券的利率字段混为一个“利率”。

## 历史、分页与缓存

公开接口：`https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/od/auctions_query`。按所选日期字段过滤，排序为主日期、CUSIP、另一日期；每页1000行、最多300页。逐页校验总数、行数、日期范围、事件顺序、重复、数值与官方标记。分页变化或失败不返回部分结果，未知类别不静默归类。

复用 `provider/fiscaldata.Client` 的六小时验证缓存，目录为 `~/.sec/cache/fiscaldata`，URL及参数参与键。`--refresh` 强制获取，错误响应不覆盖旧缓存；`--no-cache` 不读写缓存。拍卖与DTS因URL不同独立缓存。

历史字段的空字符串／`null` 保持 `null`，与真实零值区分；核心金额字段整个消失时视为响应结构变化，不能伪装为历史缺失。`first_date`、`last_date` 是查询结果按所选口径的有效起止日期，空查询时为空字符串。`data_version=current-visible-revision` 表示当前可见历史；`published_at`、`revision_id` 为 `null`，不伪造逐期公布时分或历史版本。

真实样本为1998年7—8月及2026年9月官方查询响应，均压缩为单行。测试覆盖同CUSIP再发行保留、两种日期过滤、SOMA／FIMA精确标记、美元面值、历史空值／真实零、FRN／TIPS、坏日期与金额、分页截断／重复／总数变化、缓存与失败刷新、取消、命令JSON和终端格式。

拍卖日跨页测试特意让下一页的发行日早于上一页，确认只按拍卖口径排序，并校验跨页重复／乱序失败时不返回部分结果。未来日期边界测试确认今日拍卖、未来发行的事件保留原发行日，且不混入今日发行口径；未来拍卖记录和主动查询未来区间仍被拒绝。

## 官方参考

- [财政部Auction Search](https://www.treasurydirect.gov/auctions/auction-query/)：覆盖与字段扩充说明。
- [财政部再发行说明](https://www.treasurydirect.gov/auctions/reopenings/)：同券再发行保留到期日和票息，发行日与价格可能不同。
- [财政部拍卖FAQ](https://www.treasurydirect.gov/help-center/faqs/auction-faqs/)：拍卖与中标口径。
- [官方查询使用说明](https://www.treasurydirect.gov/auctions/auction-query/auction-query-help/)：证券类型、日期筛选与导出。
