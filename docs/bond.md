# 美国国债收益率数据

## 数据来源

[美国财政部官方网站](https://home.treasury.gov/resource-center/data-chart-center/interest-rates/TextView?type=daily_treasury_yield_curve&field_tdr_date_value=2026)

XML API 接口格式：
```
https://home.treasury.gov/resource-center/data-chart-center/interest-rates/pages/xml?data=daily_treasury_yield_curve&field_tdr_date_value={year}
```

数据覆盖从 1990 年至今的每日国债收益率曲线，采用 Atom feed + 内嵌 XML 的双层结构。

## 实现架构

```
cmd/bond/bond.go          -- CLI 命令入口 (sec bond / sec b)
cmd/bond/bond_history.go  -- 历史数据命令 (sec bond-history / sec bh)
provider/bond/bond.go     -- 数据获取与解析
```

### provider/bond

- `QueryBond(ctx, req)` — 主查询函数，支持按日期范围过滤，自动处理跨年查询
- `fetchYearData(ctx, year)` — 按年份请求 US Treasury XML API
- `parseAtomFeed(data)` — 双层 XML 解析：
  1. 外层：Atom feed (`encoding/xml` → `atomFeed`/`atomEntry`)
  2. 内层：`<content>` 中的 `m:properties` → `treasuryProperties`

### cmd/bond

- `sec bond` (`sec b`) — 获取最近 10 个交易日数据，取最新一条展示
- `sec bond-history` (`sec bh`) — 默认最近 30 天历史数据，支持 `-b`/`-e` 参数指定范围
- `printBondYield` / `printBondHistory` — 使用 `tablewriter.Rich()` 渲染表格，10年期收益率根据涨跌着色（红涨绿跌）

## 解析的全部字段

| XML 字段 | Go 字段 | 说明 |
|---------|--------|------|
| `d:BC_1MONTH` | BC1Month | 1个月 |
| `d:BC_3MONTH` | BC3Month | 3个月 |
| `d:BC_6MONTH` | BC6Month | 6个月 |
| `d:BC_1YEAR` | BC1Year | 1年 |
| `d:BC_2YEAR` | BC2Year | 2年 |
| `d:BC_3YEAR` | BC3Year | 3年 |
| `d:BC_5YEAR` | BC5Year | 5年 |
| `d:BC_7YEAR` | BC7Year | 7年 |
| `d:BC_10YEAR` | BC10Year | 10年（基准）|
| `d:BC_20YEAR` | BC20Year | 20年 |
| `d:BC_30YEAR` | BC30Year | 30年 |

YClose/Change/ChangeRate 基于 BC10Year 计算（10年期为市场基准利率），用于表格着色和变动(bp)列。

## 输出示例

### sec bond（默认）

```
日期       | 1个月  | 3个月  | 6个月  | 5年    | 10年   | 前值   | 变动(BP)
2026-05-08 | 3.71% | 3.69% | 3.74% | 4.02% | 4.38% | 4.41% | -3.0
```

### sec bond-history（默认最近30天）

```
日期       | 1个月  | 3个月  | 6个月  | 5年    | 10年   | 变动(BP)
2026-05-01 | 3.71% | 3.68% | 3.71% | 4.02% | 4.39% | -1.0
2026-05-04 | 3.71% | 3.70% | 3.76% | 4.08% | 4.45% | +6.0
...
```

### sec bond-history -b 20260501 -e 20260508

```
日期       | 1个月  | 3个月  | 6个月  | 5年    | 10年   | 变动(BP)
2026-05-01 | 3.71% | 3.68% | 3.71% | 4.02% | 4.39% | -1.0
2026-05-04 | 3.71% | 3.70% | 3.76% | 4.08% | 4.45% | +6.0
2026-05-05 | 3.70% | 3.69% | 3.75% | 4.08% | 4.43% | -2.0
2026-05-06 | 3.70% | 3.69% | 3.74% | 3.99% | 4.36% | -7.0
2026-05-07 | 3.72% | 3.69% | 3.74% | 4.04% | 4.41% | +5.0
2026-05-08 | 3.71% | 3.69% | 3.74% | 4.02% | 4.38% | -3.0
```

变动(bp) 为基点（basis points），1bp = 0.01%。
收益率上涨显示红色，下跌显示绿色。
