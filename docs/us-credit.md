# 美国信用指标

银行信贷、贷款标准与家庭偿债负担分别查询，不混合成同一指标，也不在本轮进行中美比较。三个系列由美联储编制，经 FRED 公开 CSV 下载，无需 API key。

```bash
sec macro us bank-credit --begin 202501 --end 202610
sec macro us lending-standards --begin 2025Q1 --end 2026Q4
sec macro us debt-service --begin 2025Q1 --end 2026Q3 --json
sec macro us bank-credit --refresh
sec macro us debt-service --no-cache
```

## 统计口径

| 命令 | FRED 系列 | 频率和原始单位 | 季调与折年 | 核验起点 |
| --- | --- | --- | --- | --- |
| `bank-credit` | `LOANINV` | 月平均余额，十亿美元 | 季调、未折年 | 1947-01 |
| `lending-standards` | `DRTSCILM` | 季度调查，净百分比 | 未季调、未折年 | 1990Q2 |
| `debt-service` | `TDSP` | 季度，百分比 | 季调、未折年 | 2005Q1 |

**银行信贷**为 H.8 商业银行信贷余额，包含证券、贷款和租赁。覆盖商业银行的美国国内办公室及美国境内外资相关机构，排除储贷机构、信用合作社和消费金融公司等，不能当作所有金融机构贷款或全社会融资总量。月度水平来自周三余额的按比例平均，`measurement=monthly_average_stock`，不是月末余额或当月新增信贷；不标记虚构的月末余额日期。

命令以当前月平均水平计算未年化同比与月环比。H.8 官方增速还进行非银行结构活动、会计规则等断点调整并折年，两者不能混称。JSON 标记 `change_method=level_ratio_not_break_adjusted`，终端也说明不是 H.8 官方增速。分析机构变化和会计规则影响时应查看 H.8 的历史备注。

**贷款标准**使用 SLOOS 国内受访银行对大中型企业工商贷款（C&I）的回答。净收紧比例为报告收紧的银行比例减去报告放松的比例，范围为 -100% 至 100%；正数表示净收紧，负数表示净放松，零是有效值。它不是贷款利率、拒贷率、绝对标准水平或贷款金额份额。现行大中型企业定义为年销售额至少 5000 万美元；调查样本会变化。

`period_basis=survey_quarter` 沿用 FRED 调查季度，不擅自移动日期，也不将其视为完整季度活动的统计期。例如 FRED 的 2026Q3 对应 7 月调查，询问过去三个月的变化，通常对应第二季度，调查结果还可能晚于调查期发布。程序不假定每条历史调查具有相同精确问卷起止日。

**家庭偿债比率**为按计划须偿付的抵押与消费债务款项占可支配个人收入的比率，是家庭部门总量估计，不是债务余额收入比、违约率或典型家庭的负担。该比率包括抵押贷款及消费贷款等计划付款，不代表所有家庭已经实际支付的金额。

2024Q2 发布起采用征信数据方法，按该方法计算的历史从 2005Q1 起。新方法抵押贷款付款包含随还款托管的房产税和保险，旧方法不含这些项目；旧方法的 1980 年起档案不与当前系列拼接。`methodology=credit_bureau_based_dsr` 适用于整个当前历史窗口，不能把 2024 年方法切换发布时点误当作水平序列的起点。此项也不是包含租金等额外支出的 Financial Obligations Ratio。

## 比较基期与变化单位

- 银行信贷：`(当期/基期 - 1) × 100`，基期为严格上月、上年同月；增速未年化。
- 贷款标准、偿债比率：当期水平减去严格上季、上年同季水平，单位为**百分点**，不相除计算百分比增长。净收紧比例为零或负数时仍可计算百分点差。
- JSON 分开保留 `previous_change_percent` / `year_ago_change_percent` 和 `previous_change_pp` / `year_ago_change_pp`；不适用的两项为 `null`，全局另列 `change_unit`。
- `previous_period_base`、`year_ago_base` 保留严格期间、原始水平和字符串，基数可以在请求窗口外。缺少严格基期时返回空值，不跨缺口寻找最近有效值；银行信贷百分比变化另要求基数非零。
- 源空字符串、`.` 保留为缺失，真实零不转为空。银行信贷余额和偿债比率拒绝负值；SLOOS 允许 [-100,100] 内的负值。所有源值和可计算变化必须有限，避免衍生溢出污染缓存。

终端仅四列：所属期、水平、较上期变化及较上年同期变化，表头明确百分比或百分点。长期方法、来源、原值与逐条缺失说明通过 `--json` 查看。月度日期显示 `YYYY-MM`，季度显示 `YYYYQn`。

## 覆盖、日期和修订

2026-10-29 核验完整快照：银行信贷 957 月，1947-01 至 2026-09；贷款标准 146 季，1990Q2 至 2026Q3；当前方法偿债比率 86 季，2005Q1 至 2026Q2。运行时 `first_period` / `last_period` 从各自非空值计算，与请求窗口分开记录。

银行信贷默认 UTC 上个月及前 11 个月；两个季度指标默认 UTC 上一完整季度及前 7 季。默认起点在各自已核验起点截短；显式请求更早、倒序、非法日期或晚于当前 UTC 月/季会报错。日期窗口每期均保留，尚未提供的水平为 `null`，不提前填值；缺失本身不能证明官方尚未发布。

`data_version=current_visible_revision` 表示下载时可见的修订历史。CSV 无逐期发布时间或修订批次时，`published_at`、`revision_id` 为 `null`，`fetched_at` 仅为抓取时间，不能用于历史时点回测中的可得时间替代。

## 请求、缓存和测试

独立下载 `https://fred.stlouisfed.org/graph/fredgraph.csv?id=<id>`，复用 `Client.csvSnapshot` 六小时缓存：

- `~/.sec/cache/fred/us-credit-bank.json`
- `~/.sec/cache/fred/us-credit-standards.json`
- `~/.sec/cache/fred/us-credit-debt-service.json`

完整快照的列名、日期、频率、重复期间、有效观测、有限值、符号范围与严格基期变化在缓存命中和写入前均校验。坏缓存重新下载；异常刷新不覆盖有效缓存；过期、未来时间戳和来源不匹配的缓存不复用；下载失败不静默回退旧缓存。`--refresh` 跳过读取并更新，`--no-cache` 不读写磁盘。

`provider/fred/testdata/us-credit-*.csv` 为本次核验下载的完整原始 CSV，统一 LF 换行，用于离线解析、数据口径与命令回归。

```bash
go test ./provider/fred ./cmd/macro -run Credit -count=1
```

## 官方来源

- [FRED LOANINV](https://fred.stlouisfed.org/series/LOANINV) 与 [美联储 H.8 说明](https://www.federalreserve.gov/releases/h8/about.htm)：月度平均、统计范围和官方断点调整增速。
- [H.8 当前表及历史备注入口](https://www.federalreserve.gov/releases/h8/current/)：银行信贷、证券、贷款与租赁的区别。
- [FRED DRTSCILM](https://fred.stlouisfed.org/series/DRTSCILM) 与 [SLOOS](https://www.federalreserve.gov/data/sloos.htm)：国内银行的调查系列。
- [2026 年 7 月 SLOOS](https://www.federalreserve.gov/data/sloos/sloos-202607.htm)：调查季度、回顾期间、净收紧定义和企业规模定义。
- [FRED TDSP](https://fred.stlouisfed.org/series/TDSP) 与 [DSR 当前方法](https://www.federalreserve.gov/releases/dsr/about.htm)：当期方法覆盖和征信数据口径。
- [旧方法档案与切换说明](https://www.federalreserve.gov/releases/housedebt/)：明确新旧方法区别，不拼接历史。
