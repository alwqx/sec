# 美国住宅建设与设备投资

美国投资相关指标分为两个命令：`investment` 查询月度住宅开工、建筑许可住房套数；`equipment-investment` 查询季度私人非住宅设备投资。月度与季度使用独立日期参数，不放入同一时间序列。数据经 FRED 公开 CSV 转接 Census/HUD 与 BEA 官方统计，无需 API key。

```bash
# 月度住宅开工，默认 component=housing-starts
sec macro us investment --begin 202501 --end 202609
sec macro us investment --component building-permits --begin 202501 --end 202609 --json

# 季度设备投资，默认 price-basis=real
sec macro us equipment-investment --begin 2025Q1 --end 2026Q2
sec macro us equipment-investment --price-basis nominal --begin 2025Q1 --end 2026Q2 --json

sec macro us investment --refresh
sec macro us equipment-investment --no-cache
```

## 数据口径与覆盖

| 命令与选项 | 官方 FRED 系列 | 频率 | 单位 | 最早有效期 |
| --- | --- | --- | --- | --- |
| `investment --component housing-starts` | [HOUST](https://fred.stlouisfed.org/series/HOUST) | 月度 | 千套，季调折年 | 1959-01 |
| `investment --component building-permits` | [PERMIT](https://fred.stlouisfed.org/series/PERMIT) | 月度 | 千套，季调折年 | 1960-01 |
| `equipment-investment --price-basis real` | [Y033RX1Q020SBEA](https://fred.stlouisfed.org/series/Y033RX1Q020SBEA) | 季度 | 十亿美元，2017 年链式美元，季调折年 | 2007Q1 |
| `equipment-investment --price-basis nominal` | [Y033RC1Q027SBEA](https://fred.stlouisfed.org/series/Y033RC1Q027SBEA) | 季度 | 十亿美元，当期美元，季调折年 | 1947Q1 |

所有水平都是 **季调折年（SAAR）**，不是该月或该季度实际发生的数量、金额。住房套数不是建筑物数量，也不是投资金额；不能将月度住房套数与季度设备金额加总为“总投资”。两个设备序列的 BEA 账户代码分别为 `Y033RX`、`Y033RC`。

`HOUST` 统计新建私人住宅单元的开工：以地基或基础开始开挖为准，多户建筑开始开挖时计入其全部住房单元。自 1992-09 起，还包括在原有地基上完全重建的住房。Census 新住宅建设统计排除公共住房及 HUD-code 活动房等特定范围，不能理解为全部住房供给。

`PERMIT` 统计签发许可地区获准新建的私人住房单元，不是许可证张数，不覆盖无需建筑许可的地区。许可与开工是两个建设阶段，获批不等于已经开工。许可调查总体历史上发生过更新，2023-01 起改为每年更新辖区范围；2025-01 所属期起还调整了缺报插补方法。跨这些时间分析应结合官方方法说明。

设备投资取自 BEA NIPA 中**私人非住宅固定投资的设备部分**，包括企业等主体用于生产的设备投资流量；不包括建筑和知识产权产品，不是设备订单、资本品出货、资本存量或居民耐用品消费。实际链式美元分项通常不具有可加性，不与其他链式分项相加构造合计。

2026-10-28 获取的完整 CSV 中，实际设备投资 `Y033RX1Q020SBEA` 的有效水平仅从 **2007Q1** 开始；名义设备序列则从 1947Q1 开始。组合下载中 1947Q1 至 2006Q4 的实际列为空，不将名义值当作实际值，也不使用停用的“设备与软件”序列拼接历史。后续如需更早实际设备水平，应另行核实 BEA 的独立原始档案。

## 日期、变化与缺失

月度日期使用 `YYYYMM`，表格显示 `YYYY-MM`；默认截至 UTC 上个月及前 11 个月。季度日期使用 `YYYYQ1` 至 `YYYYQ4`；默认截至 UTC 上一完整季度及前 7 季。默认窗口会按所选序列的最早期截短；显式请求早于该序列起点时报错。允许查询当前 UTC 月份或季度，但拒绝更晚时间。

默认截至期不保证已有公布值。请求窗口中缺失的月份、季度仍保留行，`level=null`，表格显示 `—`。缺失可能来自未公布或源数据缺口，程序不推断具体原因，不将缺失当作零，也不缩短到最近有值的期间。`first_period`、`last_period` 表示所选系列在当前快照中有效水平的实际覆盖范围，与请求窗口分开记录。

- 同比：`(当期水平 / 上年同期水平 - 1) × 100`，严格使用上年同月或上年同季。
- 月环比或季环比：`(当期水平 / 上期水平 - 1) × 100`，严格使用上个月或上一季度。
- 两种增速都**未年化**；水平采用 SAAR 不意味着增速也应再次年化。
- 基期在查询窗口外时，从同一份完整快照读取。缺少严格基期或基期为零时返回 `null`，不跨缺口寻找最近观测。
- JSON 保留 `raw_level` 原始字符串、`previous_period_base` 与 `yoy_base`；源空字符串、`.` 缺失标记保留原样。缺整行时保留所需期间，原值为空字符串。
- 源值必须有限且非负；计算后的同比、上期增速也必须有限，溢出直接报错。

## 溯源与缓存

JSON 明确记录 `frequency`、`measure`、`price_basis`、`unit`、`reference_year`、`seasonally_adjusted`、`level_annualized`、`growth_annualized`、官方系列 ID、BEA 代码、原始来源和 FRED 下载来源。住宅套数不属于价格序列，`price_basis=not_applicable`、`reference_year=null`；实际设备投资的参考年为 2017，名义设备投资没有参考年。

数据代表**当前可见修订后的历史**，`data_version=current_visible_revision`。`fetched_at` 是采集时间，不能当作公布时间。CSV 没有逐期实际公布时间、版本编号或完整 vintage 档案，因此 `published_at`、`revision_id` 为 `null`，不能用于严格的历史时点回测。

两类完整快照分别缓存六小时：

- `~/.sec/cache/fred/us-investment-housing.json`：同时包含 `HOUST`、`PERMIT`，切换住宅分类时复用。
- `~/.sec/cache/fred/us-investment-equipment.json`：同时包含设备名义、实际系列，切换价格口径时复用。

先校验全部列、日期频率、重复期间、缺失标记、有限非负水平和完整快照可计算的严格基期增速，再原子写入缓存。缓存与请求来源 URL 绑定；坏 JSON、坏 CSV、过期和未来时间戳缓存都会重新请求。刷新遇到错误列、无效数值或衍生增速溢出时不覆盖原有效缓存。上游失败直接报错，不以过期缓存替代。

`--refresh` 跳过读取缓存并更新；`--no-cache` 不读写磁盘。离线测试使用 `provider/fred/testdata/us-investment-housing.csv`、`us-investment-equipment.csv`，均为 2026-10-28 从上述组合 FRED CSV URL 下载的真实完整快照。网页缓存与 CSV 可能出现不同版本，测试以保存的 CSV 原值为准。

```bash
go test ./provider/fred ./cmd/macro -run '(USInvestment|InvestmentOfficial|InvestmentCache|InvestmentCalendar|InvestmentEarliest|InvestmentRejects)' -count=1
```

官方说明：

- [Census：New Residential Construction](https://www.census.gov/construction/nrc/index.html)
- [Census：Survey of Construction 定义](https://www.census.gov/construction/soc/definitions.html)
- [Census：Building Permits Survey 方法与总体更新](https://www.census.gov/construction/bps/methodology.html)
- [BEA：NIPA Handbook 第六章，私人固定投资](https://www.bea.gov/resources/methodologies/nipa-handbook/pdf/chapter-06.pdf)
