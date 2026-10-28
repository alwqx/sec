# 美国实际GDP与发布版本

## 使用方法

```bash
sec macro us gdp
sec macro us gdp --begin 2025Q1 --end 2026Q2
sec macro us gdp --begin 2026Q1 --end 2026Q2 --vintages
sec macro us gdp --begin 2026Q2 --end 2026Q2 --vintages --json --no-cache
```

季度参数使用 `YYYYQ1` 至 `YYYYQ4`。默认截至 UTC 上一个已结束季度，展示包括截至季度在内的八个季度；开始季度下限为 `1947Q1`。当前季度可以显式查询，尚未发布时保留该季度并返回空值，不向前补齐其他季度。

普通查询从 FRED 获取抓取时可见的修订后实际GDP水平和官方环比年化增速。`--vintages` 额外请求 BEA 官方发布版本档案，展示各次发布的实际增长率和发布日期。版本档案可用范围与当前水平历史不同。

## 官方来源与口径

| 字段 | 来源 | 含义 |
| --- | --- | --- |
| `level` | FRED `GDPC1`；BEA `A191RX` | 实际GDP，十亿美元、2017年链式美元，季度季调折年水平（SAAR），1947Q1起 |
| `qoq_annualized_percent` | FRED `A191RL1Q225SBEA`；BEA `A191RL` | 官方实际GDP季调环比年化增速，1947Q2起；直接采集发布值 |
| `qoq_percent` | 同一份 `GDPC1` 快照 | `(当季水平 / 上季水平 - 1) × 100`，未年化 |
| `yoy_percent` | 同一份 `GDPC1` 快照 | `(当季水平 / 上年同季水平 - 1) × 100`，未年化 |
| `qoq_base`、`yoy_base` | 同一份 `GDPC1` 快照 | 严格上季、上年同季的原始水平、季度与原文 |
| `vintages` | BEA Vintage History XLSX | 各次发布的实际环比年化增速、名义GDP水平及发布日期 |

来源：

- [FRED 实际GDP GDPC1](https://fred.stlouisfed.org/series/GDPC1)
- [FRED 官方实际GDP环比年化增速](https://fred.stlouisfed.org/series/A191RL1Q225SBEA)
- [FRED 两序列 CSV](https://fred.stlouisfed.org/graph/fredgraph.csv?id=GDPC1,A191RL1Q225SBEA)
- [BEA GDP主页](https://www.bea.gov/data/gdp/gross-domestic-product)，Previously Published Estimates 下提供版本档案
- [BEA GDP/GDI Vintage History XLSX](https://apps.bea.gov/national/xls/gdp-gdi-vintage-history.xlsx)
- [BEA GDP说明](https://www.bea.gov/resources/learning-center/what-to-know-gdp)：季度GDP默认以折年口径发布，名义与实际金额含义不同

季调折年水平用于季度间比较，不是该季度实际发生的金额。不能直接与中国GDP当季金额比较，也不应把年化环比与未年化环比直接比较。官方年化增长率直接取独立系列，不用已四舍五入的GDP水平重算，以保留官方发布精度。

## 发布版本的范围与限制

2026-10-28 开发核验的 BEA 档案标注 `Last Updated September 30, 2026`，覆盖 **2002Q1—2026Q2，共98个季度**。运行时元数据 `vintages_first_period`、`vintages_last_period` 来自实际解析到的档案季度范围，不代表用户查询窗口。

每个 `vintages` 元素保留以下字段：

- `estimate`：官方版本名称原文。常见为 `Advance`、`Second`、`Third`、`Revised`；较早年份有 `Preliminary`、`Final`，2025Q3有 `Initial`、`Updated`。保留官方分类，不根据顺序臆造初值、二次或三次标签。
- `release_date`：档案给出的真实发布日期，格式 `YYYY-MM-DD`。档案没有具体时分，`published_at=null`，不推算成纽约时间08:30等时间点。
- `qoq_annualized_percent`：该次发布的实际GDP环比年化增速。
- `nominal_level`：档案 `Billions of Current Dollars / GDP` 列，十亿美元当期美元、季调折年水平，**属于名义GDP**，单位单独见 `vintages_nominal_unit`。
- `real_level`：始终 `null`。该档案没有各发布版本的实际GDP金额，不能将名义金额、当前修订金额或推算金额填入。
- `raw_nominal_level`、`raw_qoq_annualized`、`raw_release`、`note`：保留官方原文，包括 `GDP not open for revision` 或 `GDP not revised` 等说明。这些记录表示当次官方档案中的值，不代表每条记录都修订了GDP。

表格用中文说明版本名称和常见发布注释，同时保留版本英文标识便于核对；完整英文原文可通过 JSON 查阅。各季度版本按发布日期升序展示。

请求2002Q1以前的版本或尚未进入档案的季度时，对应 `vintages=[]`，季度说明标记“BEA发布版本档案未覆盖该季”。没有启用 `--vintages` 时 `vintages=null`，表示未请求。只有在来源成功解析且确认没有该季度记录后才返回空数组；显式请求档案但下载或解析失败时返回错误。

**尚未实现的范围**：2002Q1以前的初值/修订档案，以及每个发布版本的实际GDP水平和由这些水平计算的同比。当前实现不提供任意历史日期的完整“当时可见”GDP水平快照，不应作为无前视偏差的历史回测输入。

## 缺失值、元数据与缓存

CSV中空字符串、`.` 都视为缺失。源值、严格季度基期缺失或基期为零时，相关计算字段为 `null`，表格显示 `—`；保留零水平，不改成缺失。季度缺口不会用前一个有值季度顶替。负增速正常保留；负水平、NaN、无穷值、重复季度、错列和非季度首日日期都会报错。

当前水平的元数据包括BEA生产者、FRED分发者、系列ID、账户代码、季调/折年口径、2017参考年、原始来源和抓取时间。`data_version=latest-available` 仅表示抓取时可见的修订版本；FRED CSV没有逐期公布时间或修订编号，因此顶层 `published_at`、`revision_id` 保留 `null`。`fetched_at` 不代表公布时间。

两份完整快照分别缓存在 `~/.sec/cache/fred/gdp.json` 和 `gdp-vintages.json`，有效期六小时，缓存键核对来源，命中时也重新校验数据。BEA二进制工作簿通过JSON的二进制编码保存，避免字符串转换损坏ZIP字节。仅在下载完整且解析通过后原子替换缓存；失败不覆盖已存在文件，也不静默回退过期内容。

`--refresh` 强制重新获取已请求的数据；`--no-cache` 跳过缓存读写。默认不请求版本档案，因此BEA档案接口故障不会影响普通当前值查询。HTTP请求传递命令上下文并设30秒超时，限制响应大小；XLSX读取只使用Go标准库，不引入Excel依赖。

## 开发验证

`provider/fred/testdata/us-gdp.csv` 与 `us-gdp-vintages.xlsx` 为2026-10-28从上述官方公开下载地址采集的原始测试快照。它们仅用于离线回归测试，生产查询不回退到这些测试文件。

测试覆盖官方系列与档案解析、严格季度基期、缺失与零基期、负增速、错误格式、缓存复用与失败保护、UTC跨季、命令参数及JSON/表格输出。档案测试核对2026Q2初值1.5%、二次1.5%、三次2.2%，并确认历史实际水平为空及2025Q3特殊标签未被改写。
