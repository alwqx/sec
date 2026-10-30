# 汇率采集

`sec fx` 独立提供人民币中间价、在岸市场参考汇率、在岸／离岸人民币和港币行情，以及 FRED 长期历史。所有货币对使用 `基础货币/计价货币`：`USD/CNY` 表示1美元兑换的在岸人民币数量，`USD/CNH` 对应离岸人民币市场。CNH 是离岸人民币市场标识，不能与 CNY 数据合并。

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

来源没有日期记录时不补周末或休市日；来源列出的缺失值及被识别的异常记录保留为空。不同价格类型、时区或来源的历史不互相补齐。CNY/CNH价差和交叉汇率计算须另行对齐时点，当前命令不自动套算，也不支持用反向报价替换请求币种。

## 测试与后续来源

Go测试使用精简官方样本和HTTP mock，覆盖币种单位、编码、报价时间、日线字段顺序、坏行与缺失值、源覆盖边界、跨月与多页查询、分页重复／截断／总数变化、缓存命中／失败刷新／损坏／未来时间、请求取消、非法参数、四类JSON输出和终端格式。样本仅用于离线回归，线上响应仍可能修订。

HKMA正式汇率API可用于后续港币历史核验，但日频观测不等于每日实时更新，其 `cny` 字段未经口径核验不能认作CNH。[HKMA接口文档](https://apidocs.hkma.gov.hk/documentation/market-data-and-statistics/monthly-statistical-bulletin/er-ir/er-eeri-daily/)

TMA的USD/CNH、USD/HKD基准有数据使用许可及历史访问要求，当前不将其作为免费默认采集源。[TMA许可说明](https://www.tma.org.hk/TMA%20Notice%20on%20Licensing%20Regime%20with%20FAQ.pdf)
