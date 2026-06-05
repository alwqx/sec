# sec 功能路线图

## 现有功能回顾

| 命令                           | 功能                             | 数据源          |
| ------------------------------ | -------------------------------- | --------------- |
| `sec search` / `sec s`         | 证券代码搜索                     | 新浪            |
| `sec info` / `sec i`           | 公司基本信息 + 分红              | 新浪            |
| `sec quote` / `sec q`          | 实时行情（单只/多只/实时刷新）   | 新浪            |
| `sec quote-history` / `sec qh` | 历史 K 线表格                    | 东方财富        |
| `sec kline` / `sec kl`         | 终端蜡烛图（Unicode）            | 东方财富        |
| `sec bond` / `sec b`           | 美国国债收益率                   | Treasury.gov    |
| `sec bond-history` / `sec bh`  | 国债收益率历史                   | Treasury.gov    |
| `sec metal`                    | 黄金价格（Au99.99）              | 上金所          |
| `sec bs`                       | 财务报表查询 + CSV/JSON 导出     | 东方财富        |
| `sec bsd`                      | 年报 PDF 下载                    | 巨潮资讯网      |
| `sec val` / `sec v`            | 估值分析（PE/PB/PEG/DCF）        | 东方财富 + 新浪 |
| `sec st <method>`              | 技术策略信号（MA/MACD/RSI/布林） | 东方财富        |
| `sec upgrade`                  | 自我更新                         | GitHub Releases |

## 可扩展功能

### 一、市场全景

**1.1 指数行情**

对标 Wind/Choice 的指数监控面板。

```bash
sec index                    # 主要指数一览（上证、深证、创业板、科创 50、沪深 300）
sec index 000001             # 上证指数详情
sec index --realtime         # 实时刷新
```

数据源：新浪/东方财富指数接口（已有 provider 基础）。

**1.2 市场宽度 (Market Breadth)**

```bash
sec breadth                 # 涨跌家数、涨跌停统计
sec breadth --sector        # 行业板块涨跌排行
```

数据源：东方财富板块行情 API。常用指标：上涨/下跌/平盘家数、涨停/跌停数、成交额。

**1.3 北向资金 (North-bound Flow)**

```bash
sec northbound              # 北向资金净流入（沪股通+深股通）
sec northbound --history    # 历史净流入趋势
```

数据源：东方财富沪股通/深股通接口。

---

### 二、选股与筛选

**2.1 多因子筛选器 (Stock Screener)**

对标 Finviz/TradingView 的筛选器。

```bash
sec screen --pe-max 15 --pb-min 0.5 --pb-max 2.0 --roe-min 15 --market cap-min 100
sec screen --sector 银行 --dividend-yield-min 3
sec screen --preset graham       # 预设：格雷厄姆选股
sec screen --preset dividend     # 预设：高股息
sec screen --preset growth       # 预设：成长股
```

需要：全市场股票列表 + 批量财务数据。可复用现有估值计算逻辑。

**2.2 预设筛选模板**

| 模板         | 条件                                         |
| ------------ | -------------------------------------------- |
| `graham`     | PE < 15, PB < 1.5, 格雷厄姆数 > 股价         |
| `dividend`   | 股息率 > 3%, PE < 15, 派息率 < 80%           |
| `growth`     | PEG < 1, 营收增速 > 20%, ROE > 15%           |
| `deep-value` | PE < 行业平均×0.5, PB < 1, 股价 < 格雷厄姆数 |
| `quality`    | ROE > 20%, 毛利率 > 30%, 负债率 < 50%        |

---

### 三、对比分析

**3.1 同行业对比**

```bash
sec compare 600036 601398 601939  # 招商 vs 工行 vs 建行
sec compare --sector 银行 --top 5  # 行业 Top5 对比
```

对比维度：PE、PB、ROE、市值、股息率、营收增速。

**3.2 历史估值对比**

```bash
sec val 600036 --compare          # 当前估值 vs 同行业
sec val 600036 --history          # PE/PB 历史走势图
```

---

### 四、组合与回测

**4.1 策略回测 (Backtesting)**

对标 QuantConnect/Zipline 的轻量版。

```bash
sec backtest ma 600036 --fast 5 --slow 20 --from 2020-01-01
sec backtest rsi 600036 -p 14 --from 2020-01-01 --capital 100000
```

输出：

- 总收益率 / 年化收益率
- 最大回撤
- 夏普比率
- 胜率（盈利交易占比）
- 交易次数
- 收益曲线（`--chart` 模式）

**4.2 组合跟踪 (Watchlist)**

```bash
sec watch add 600036 601398 600519    # 添加自选
sec watch quotes                        # 自选行情一览
sec watch remove 600036                 # 删除
```

数据持久化到 `~/.sec/watchlist.json`。

---

### 五、可视化增强

**5.1 K 线叠加指标**

```bash
sec kline 600036 --ma 5,20,60          # K 线 + 均线
sec kline 600036 --boll 20,2           # K 线 + 布林带
sec kline 600036 --macd                # K 线 + MACD 副图
```

复用 `render` 包，在蜡烛图上叠加均线/布林带，副图显示成交量和 MACD。

**5.2 策略信号标注**

```bash
sec kline 600036 --strategy ma --fast 5 --slow 20
# K 线图上标注买入/卖出箭头
```

---

### 六、公告与新闻

**6.1 公告速览**

```bash
sec announcements 600036                # 近期公告列表
sec announcements 600036 --type 年报     # 按类型筛选
sec announcements --latest              # 全市场最新公告
```

已有 CNINFO provider 基础，只需补充展示层。

**6.2 高管增减持**

```bash
sec insider 600036                      # 高管增减持记录
```

数据源：东方财富高管持股变动接口。

---

### 七、更多资产类别

**7.1 ETF 数据**

```bash
sec etf 510050                           # ETF 净值/折溢价
sec etf --list                           # ETF 列表
```

**7.2 可转债**

```bash
sec cb 113011                            # 可转债行情 + 转股价值
sec cb --screen --premium-max 20         # 低溢价可转债筛选
```

**7.3 美股/港股行情**

```bash
sec quote AAPL --market us
sec kline 00700 --market hk
```

已有 provider 中的 NASDAQ/HK 交易所常量，待扩展。

---

### 八、宏观数据

**8.1 宏观经济指标**

```bash
sec macro cpi                            # CPI 数据
sec macro pmi                            # PMI 数据
sec macro gdp                            # GDP 增速
sec macro m2                             # M2 货币供应
```

数据源：东方财富宏观数据接口。

**8.2 利率与利差**

```bash
sec bond --compare                       # 中美利差对比
sec bond --yield-curve                   # 收益率曲线
```

已有美债数据，加入中国国债。

---

### 九、数据导出与报告

**9.1 一键研报生成**

```bash
sec report 600036                        # 生成综合研报
# 包含：公司概况 + 估值分析 + 财务数据 + 技术面 + 近 5 年盈利趋势
# 输出：终端预览 / PDF (--output report.pdf)
```

**9.2 批量导出**

```bash
sec export --sector 银行 --metrics pe,pb,roe,revenue --output sector.csv
```

---

### 十、配置与个性化

**10.1 配置文件 (`~/.sec/config.toml`)**

```toml
[defaults]
kline_height = 20
kline_period = 90    # 默认时间范围（天）

[watchlist]
stocks = ["600036", "000001", "600519"]

[colors]
up = "red"           # A 股习惯红涨绿跌
down = "green"
```

**10.2 数据缓存管理**

```bash
sec cache status           # 缓存状态
sec cache clear            # 清理缓存
```

---

## 优先级建议

| 优先级 | 功能                           | 理由                                      |
| ------ | ------------------------------ | ----------------------------------------- |
| P0     | 多因子筛选器 (`sec screen`)    | 直接提升选股效率，复用已有的估值/财务数据 |
| P0     | K 线叠加指标 (`--ma/--boll`)   | render 包已就绪，投入产出比最高           |
| P1     | 策略回测 (`sec backtest`)      | 策略信号已有，加入统计即可                |
| P1     | 自选组合 (`sec watch`)         | 简单文件持久化，高频使用                  |
| P1     | 指数行情 (`sec index`)         | 日常刚需                                  |
| P2     | 同行业对比 (`sec compare`)     | 多股票数据拉取逻辑                        |
| P2     | 公告速览 (`sec announcements`) | CNINFO provider 已有                      |
| P2     | ETF/可转债                     | 新资产类别，需新 provider                 |
| P3     | 美股/港股                      | 需要海外数据源调研                        |
| P3     | 宏观数据                       | 新数据源                                  |
| P3     | 研报生成                       | 综合多个模块的输出                        |
