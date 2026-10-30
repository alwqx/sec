# 汇率采集

`FX` 是 Foreign Exchange（外汇）的缩写。`sec fx` 独立提供人民币中间价、在岸市场参考汇率、在岸／离岸人民币和港币行情，以及 FRED 历史与明确标记的推导结果。所有货币对使用 `基础货币/计价货币`：`USD/CNY` 表示1美元兑换的在岸人民币数量，`USD/CNH` 对应离岸人民币市场。CNH 是离岸人民币市场标识，不能与 CNY 数据合并。

## 命令

```bash
# 最近一次参考行情：USD/CNY、USD/CNH、USD/HKD、HKD/CNY
sec fx quote
sec fx quote --pairs USD/CNY,USD/CNH --json --refresh

# 新浪来源日线：默认查询四个货币对、最近一年
sec fx history --pairs USD/CNH --begin 20240101 --end 20241231
sec fx history --pairs USD/CNY,USD/CNH,USD/HKD,HKD/CNY --json

# FRED：仅支持 USD/CNY、USD/HKD，默认查询这两个货币对
sec fx history --source fred --begin 20000101 --end 20241231 --json

# 人民币中间价：默认 USD/CNY、HKD/CNY
sec fx fixing --begin 20240101 --end 20241231

# 在岸市场参考汇率：默认北京时间16:00、USD/CNY及HKD/CNY
sec fx reference --pairs USD/CNY --at 16:00 --begin 20240101 --end 20240131
```

日期参数为包含边界的 `YYYYMMDD`，默认截至北京时间今天、从截至日期前一年开始。报价命令使用 `--pairs` 选择币种，不接受日期参数；其他命令支持日期、`--json`、`--refresh`、`--no-cache`。货币对大小写会归一化，重复或来源不支持的货币对直接报错。终端显示最多六位小数，JSON 同时保存原数值精度与原始字段。

## 来源和口径

| 命令 | 来源 | 价格与时间口径 | 覆盖及限制 |
| --- | --- | --- | --- |
| `fx fixing` | [中国货币网人民币中间价](https://www.chinamoney.com.cn/chinese/bkccpr/) | 人民银行授权外汇交易中心公布的中间价，以公布日筛选；没有网页逐日实际公布时间时不生成时间戳 | 查询自2006-01-04起；USD/CNY、HKD/CNY |
| `fx reference` | [中国货币网人民币外汇参考汇率](https://www.chinamoney.com.cn/chinese/homefxrrm/) | 对应时点前30秒内成交价格加权平均，无成交时根据买卖报价计算；不是中间价，也不是16:30收盘价 | 自2015-08-24起；USD/CNY、HKD/CNY；可选北京时间10:00、11:00、14:00、15:00、16:00 |
| `fx quote` | [新浪财经汇率页面](https://finance.sina.com.cn/money/forex/hq/USDCNH.shtml) | 最近一次上游报价，记录原始日期、时间及抓取时间；可能延迟、停留在休市前，不能宣称每次查询都有实时成交 | 四个货币对；部分在岸及交叉汇率带有“新浪财经计算得出”说明，逐条保留 |
| `fx history` | 新浪财经日线接口 | 收盘值及开高低收，保留来源交易日期；上游未提供统一收盘时刻和时区，不推断为北京时间16:30或纽约收盘 | 完整可见快照的首末日期按币种记录在 `snapshot_coverage`，不是完整市场历史保证；最新日线可能未收盘 |
| `fx history --source fred` | 美联储H.10，经FRED转接：[DEXCHUS](https://fred.stlouisfed.org/series/DEXCHUS)、[DEXHKUS](https://fred.stlouisfed.org/series/DEXHKUS) | 纽约中午电汇买入汇率；`America/New_York` 时区、12:00观测，按周发布日度数据 | 各币种独立记录当前快照有效历史起止，节假日和来源缺失值保留为 null |

中间价通过官网前端接口 `/ags/ms/cm-u-bk-ccpr/CcprHisNew` 查询，使用官网形式的货币对参数及每页10条，并按月分段。校验返回区间、币种集合、页码、总数、重复日期及最终记录完整性。返回币种必须与请求集合一致且各不重复；数值按照响应的 `searchlist` 列顺序映射，允许请求顺序与上游顺序不同。参考汇率通过 `/ags/ms/cm-u-bk-fx/RefRateHis` 按月、按币种查询，使用 `USD.CNY` 等代码与 `DD MMM YYYY` 参数；检查返回区间，防止上游恢复默认查询区间后静默返回错误数据。

新浪报价通过 `hq.sinajs.cn` 的 `fx_susdcny`、`fx_susdcnh`、`fx_susdhkd`、`fx_shkdcny` 读取，带新浪 Referer，兼容 GB18030 中文。日线使用 `NewForexService.getDayKLine`，解析字符串包装而不执行 JavaScript；原始字段顺序为日期、开盘、最低、最高、收盘。高低价关系异常时，保留日期与原始字段，汇率和 OHLC 留空并说明原因，不修改原值或让旧坏行阻断有效历史。结构、日期和非有限／非正汇率错误仍报错。

## 缓存与数据结构

数据缓存目录为 `~/.sec/cache/fx/<source>/`。新浪报价缓存15秒，历史、中间价、在岸参考汇率及FRED缓存六小时；`--refresh` 强制更新，`--no-cache` 不读写缓存。仅缓存通过语义校验的响应，刷新失败不覆盖有效旧缓存，也不把过期缓存伪装为本次成功结果。GB18030原始字节以二进制编码保存，避免中文在缓存后损坏。

JSON 保留以下信息：

- 结果：来源、价格类型、频率、时区、观测时点、日期口径、请求区间、版本类型、可见快照覆盖和说明。
- 观测：日期、基础及计价货币、在岸／离岸市场、汇率、报价买卖价或日线OHLC、上游明确的观测时间、来源URL、抓取时间、原始字段及逐条说明。
- `value`：报价接口的参考价格、中间价或参考汇率，以及日线收盘值；必须结合 `price_type` 解读。

来源没有日期记录时不补周末或休市日；来源列出的缺失值及被识别的异常记录保留为空。不同价格类型、时区或来源的历史不互相补齐。原始采集命令不自动套算，也不支持用反向报价替换请求币种；推导计算使用以下独立命令。

## 交叉汇率与在岸／离岸价差

```bash
# 同一天、纽约中午的FRED输入，推导HKD/CNY及反向汇率
sec fx cross --source fred --pairs HKD/CNY,CNY/HKD --begin 20240101 --end 20241231

# 同一天、北京时间16:00的CFETS参考汇率，推导USD/HKD
sec fx cross --source cfets --pairs USD/HKD --at 16:00 --begin 20240101 --end 20240131

# 新浪最近报价；严格模式只计算相同时间戳的所需输入
sec fx cross --source sina --pairs HKD/CNH,CNH/CNY --json
sec fx spread --json

# 显式允许不超过60秒的输入时间差，此时结果标记为近似值
sec fx cross --source sina --pairs HKD/CNH --max-skew 60s
sec fx spread --max-skew 60s --max-age 15m
```

`fx cross` 默认来源为 `fred`、目标为 `HKD/CNY`、区间为最近一年。FRED与CFETS支持USD、CNY、HKD之间的有向货币对；新浪报价另支持CNH。只使用同一个来源结果中的输入；CFETS限固定时点参考汇率，FRED限纽约中午价，新浪限带原始时间戳的报价。人民币中间价、HKMA未明确时点的日度公布值和新浪未知收盘时刻的日线均不能混入推导。

基于美元报价时，`HKD/CNY = USD/CNY ÷ USD/HKD`，倒数为 `CNY/HKD = USD/HKD ÷ USD/CNY`。CFETS来源使用同一时点的`USD/CNY`和`HKD/CNY`，因此`USD/HKD = USD/CNY ÷ HKD/CNY`。每条结果带 `is_derived: true`、`formula`、原始 `inputs` 和 `alignment`，不生成不存在的开高低收或买卖价。

日度输入按日期和已核实的固定时点对齐，不补前一交易日。报价默认 `--max-skew 0s`，要求所有所需输入时间完全一致；`--max-age`默认15分钟，最大24小时。允许的时间差上限为60秒，必须显式指定；`within-tolerance`结果保留输入时间范围和实际差值，不伪造单一观测时间。缺少输入、数值为空、过期、时间无效或时间差超限时，结果为空并注明原因。跨午夜使用带时区时间戳判断时间差，结果日期取实际所用输入的最晚日期。日度来源不接受报价专用时间差和年龄参数。

`fx spread` 只使用新浪USD/CNY、USD/CNH报价，其时间限制与新浪交叉汇率一致。终端报价时间统一显示为北京时间 `YYYY-MM-DD HH:mm:ss`，JSON保留原始RFC3339时间戳：

| 输出 | 公式与解释 |
| --- | --- |
| `difference_rmb_per_usd` | `USD/CNH − USD/CNY`；正值表示CNH相对CNY更弱，负值更强 |
| `pips` | 绝对价差×10000，1点=0.0001 |
| `relative_percent` | 绝对价差÷USD/CNY×100 |
| `relative_bps` | 绝对价差÷USD/CNY×10000；与绝对点差含义不同 |
| `implied_cnh_cny` | USD/CNY÷USD/CNH，每1离岸人民币对应的在岸人民币，仅为隐含推导值 |

价差不是两个实际可执行兑换报价之间的套利利润；近似时间对齐也不代表同一时刻成交。当前不从收盘时间未核实的日线生成历史CNY/CNH价差序列。

## 暂缓的HKMA来源

`HKMA` 是 Hong Kong Monetary Authority（香港金融管理局）。[官方汇率字段文档](https://apidocs.hkma.gov.hk/documentation/market-data-and-statistics/monthly-statistical-bulletin/er-ir/er-eeri-daily/)和[通用API文档](https://apidocs.hkma.gov.hk/documentation/)提供公开接口说明，但本轮标准查询、官方最小GET示例及带正常JSON请求头的调用均返回HTTP 405安全拦截页，未能恢复访问。因此暂不接入该来源，采集实现与线上核验保留TODO。

HKMA的 `cny` 字段没有明确在岸／离岸范围，未来接入前需要进一步核验，不能直接将其标作CNH或用于在岸／离岸价差。

## 测试与后续来源

Go测试使用精简官方样本和HTTP mock，覆盖币种单位、编码、报价时间、日线字段顺序、坏行与缺失值、源覆盖边界、跨月与多页查询、分页重复／截断／总数变化、缓存命中／失败刷新／损坏／未来时间、请求取消、非法参数、四类JSON输出和终端格式。样本仅用于离线回归，线上响应仍可能修订。

新增推导测试覆盖所有汇率方向、倒数、输入排列、来源／价格类型拒绝、正负与零价差、点差与相对基点、缺失／重复输入、严格／显式近似对齐、跨午夜、过期、未来与不一致日期，以及推导命令JSON输出和终端报价时间格式。

TMA的USD/CNH、USD/HKD基准有数据使用许可及历史访问要求，当前不将其作为免费默认采集源。[TMA许可说明](https://www.tma.org.hk/TMA%20Notice%20on%20Licensing%20Regime%20with%20FAQ.pdf)
