# 量化策略调研

## 概述

量化策略按逻辑可分为趋势跟随、均值回归、多因子选股、事件驱动四大类。以下按类别梳理常见策略，并标注每个策略需要的指标数据及 `sec` 现有支持程度。

---

## 一、趋势跟随类 (Trend Following)

### 1.1 双均线交叉 (Dual MA Crossover)

最经典的量化策略：短期均线上穿长期均线买入，下穿卖出。

**信号：**

```text
金叉（买入）: MA_short 上穿 MA_long
死叉（卖出）: MA_short 下穿 MA_long
```

常见参数组合：(5, 20)、(10, 60)、(20, 120)。

**所需数据：** 历史收盘价（日线 OHLCV）

| 数据       | sec 现有                            |
| ---------- | ----------------------------------- |
| 日线收盘价 | ✓ `sec kline` / `sec quote-history` |

### 1.2 MACD 策略

MACD = EMA_fast - EMA_slow，配合信号线 (MACD 的 EMA) 和柱状图。

**信号：**

```text
买入：MACD 线上穿信号线（金叉），且柱状图由负转正
卖出：MACD 线下穿信号线（死叉），且柱状图由正转负
```

经典参数：(12, 26, 9) — 快线 12 日 EMA、慢线 26 日 EMA、信号线 9 日 EMA。

**所需数据：** 历史收盘价

| 数据       | sec 现有 |
| ---------- | -------- |
| 日线收盘价 | ✓        |

### 1.3 海龟交易法则 (Turtle Trading)

Donchian 通道突破策略：价格突破 N 日最高价买入，跌破 N 日最低价卖出。

**信号：**

```text
买入：价格 > 过去 20 日最高价（突破入场）
卖出：价格 < 过去 10 日最低价（止损退出）
加仓：价格每上涨 0.5 ATR，加仓一次
止损：价格下跌 2 ATR，全部平仓
```

**仓位管理：** `头寸 = 账户资金 × 1% / ATR`

**所需数据：** OHLCV 日线数据

| 数据       | sec 现有                   |
| ---------- | -------------------------- |
| 日线 OHLCV | ✓                          |
| ATR        | ✓ 可计算 （从 H/L/C 推导） |

### 1.4 动量策略 (Momentum)

买入过去 N 个月涨幅最高的股票，卖出涨幅最低的股票（或仅买入不做空）。

**信号：**

```text
买入：过去 12 个月收益率排名前 10%的股票
卖出：过去 12 个月收益率排名后 10%的股票
```

**所需数据：** 历史月度收益率

| 数据           | sec 现有     |
| -------------- | ------------ |
| 月线收盘价     | ✓ 从日线聚合 |
| 多股票横向对比 | ✗ 需股票池   |

---

## 二、均值回归类 (Mean Reversion)

### 2.1 布林带策略 (Bollinger Bands)

价格触及下轨买入、触及上轨卖出，假设价格会回归中轨。

**公式：**

```text
中轨 = MA(N)
上轨 = MA(N) + K × σ
下轨 = MA(N) - K × σ
```

经典参数：N=20, K=2。

**信号：**

```text
买入：价格触及下轨且回升
卖出：价格触及上轨且回落
```

**所需数据：** OHLCV

| 数据       | sec 现有 |
| ---------- | -------- |
| 日线 OHLCV | ✓        |

### 2.2 RSI 超买超卖策略

RSI > 70 超买（卖出信号），RSI < 30 超卖（买入信号）。

**公式：**

```text
RSI = 100 - 100 / (1 + RS)
RS = 近 N 日平均涨幅 / 近 N 日平均跌幅
```

经典参数：N=14。

**信号：**

```text
买入：RSI 从 <30 回升到 >30
卖出：RSI 从 >70 回落到 <70
```

**所需数据：** 日线收盘价

| 数据       | sec 现有 |
| ---------- | -------- |
| 日线收盘价 | ✓        |

### 2.3 配对交易 (Pairs Trading)

选择两只高度相关的股票，当价差偏离均值时做多低估方、做空高估方。

**步骤：**

1. 找出两只同行业、高相关性股票
2. 计算价差 = log(P_A) - log(P_B)
3. 标准化价差 Z = （价差 - 均值） / 标准差
4. Z > 2 做空 A 做多 B，Z < -2 反向操作

**所需数据：** 两只股票的历史价格、相关性数据

| 数据         | sec 现有         |
| ------------ | ---------------- |
| 历史价格     | ✓                |
| 同行业筛选   | ✗ 需行业分类数据 |
| 相关系数计算 | ✓ 可计算         |

---

## 三、多因子选股类 (Multi-Factor)

### 3.1 价值因子 (Value Factor)

买入低估值股票。

**常见指标：**

| 因子          | 公式              | sec 现有    |
| ------------- | ----------------- | ----------- |
| P/E（市盈率） | 股价 / EPS        | ✓ `sec val` |
| P/B（市净率） | 股价 / BVPS       | ✓           |
| P/S（市销率） | 市值 / 营收       | ✓           |
| P/FCF         | 市值 / 自由现金流 | ✓           |
| 股息率        | DPS / 股价        | ✓           |
| EV/EBITDA     | 企业价值 / EBITDA | ✓           |

**策略：** 定期（月/季度）买入全市场 P/E（或综合价值分）最低的 N 只股票。

### 3.2 质量因子 (Quality Factor)

买入高质量公司。

**常见指标：**

| 因子            | 公式                   | sec 现有    |
| --------------- | ---------------------- | ----------- |
| ROE             | 净利润 / 净资产        | ✓ `sec val` |
| ROA             | 净利润 / 总资产        | ✓           |
| 毛利率          | （营收 - 成本） / 营收 | ✓ 利润表    |
| 净利率          | 净利润 / 营收          | ✓           |
| 资产负债率      | 负债 / 资产            | ✓           |
| 经营现金流/利润 | CFO / Net Profit       | ✓           |

**策略：** 综合 ROE、毛利率、负债率等打分，买入高分股票。

### 3.3 动量因子 (Momentum Factor)

买入近期强势股。

| 因子        | 公式                        | sec 现有 |
| ----------- | --------------------------- | -------- |
| 1 月动量    | 近 1 月收益率               | ✓        |
| 3 月动量    | 近 3 月收益率               | ✓        |
| 12-1 月动量 | 近 12 月（剔近 1 月）收益率 | ✓        |
| 换手率      | 成交量 / 流通股本           | ✓        |

### 3.4 低波动因子 (Low Volatility)

买入低波动、低 Beta 股票。

| 因子       | 公式                                   | sec 现有     |
| ---------- | -------------------------------------- | ------------ |
| 历史波动率 | 日收益率标准差                         | ✓            |
| Beta       | Cov(r_stock, r_market) / Var(r_market) | ✓ 需指数数据 |
| 最大回撤   | （峰值 - 谷值） / 峰值                 | ✓            |

### 3.5 规模因子 (Size Factor)

小市值股票长期有超额收益（Fama-French 三因子之一）。

| 因子     | 公式            | sec 现有 |
| -------- | --------------- | -------- |
| 总市值   | 股价 × 总股本   | ✓        |
| 流通市值 | 股价 × 流通股本 | ✓        |

### 3.6 多因子综合打分

将多个因子标准化后加权求和，选出综合得分最高的股票。

**常见框架：**

```text
综合得分 = w1 × Z(PE 倒数） + w2 × Z(ROE) + w3 × Z（动量） + w4 × Z（低波动率倒数）
```

权重可通过回测优化或主观设定。每季度/月度调仓一次，持有得分前 10-20 只。

**所需数据：** 全市场股票的 OHLCV + 财务数据

| 数据               | sec 现有                |
| ------------------ | ----------------------- |
| 单只股票的所有因子 | ✓                       |
| 全市场股票池       | ✗ 需股票列表 + 批量数据 |

---

## 四、事件驱动类 (Event-Driven)

### 4.1 业绩超预期策略 (Earnings Surprise)

财报发布后，实际 EPS 超出分析师预期时买入。

**信号：**

```text
买入：实际 EPS > 预期 EPS，且超预期幅度 > X%
持有：N 天后卖出（或持有到下次财报）
```

**所需数据：** 实际/预期 EPS、财报日期

| 数据         | sec 现有 |
| ------------ | -------- |
| 实际 EPS     | ✓ 利润表 |
| 分析师预期   | ✗ 需外部 |
| 财报发布日期 | ✓ 可推算 |

### 4.2 分红套利策略 (Dividend Capture)

在除权日前买入，除权日后卖出，获取分红+短期价差。

**信号：**

```text
T-2 日：买入（股权登记日前两天）
T+1 日：卖出（除权除息后一天）
```

**所需数据：** 分红公告、除权除息日期

| 数据         | sec 现有                 |
| ------------ | ------------------------ |
| 分红历史     | ✓ `sec info --dividends` |
| 未来分红计划 | ✗ 需实时公告             |

### 4.3 公告驱动 (Announcement-Based)

利用回购、增持、重大合同等公告的市场反应。

**所需数据：** 实时公告流。sec 现有的 CNINFO 接口可获取公告标题和时间，可做基础筛选。

---

## 五、高级策略

### 5.1 网格交易 (Grid Trading)

在预设价格区间内，每下跌一定幅度买入一份、每上涨一定幅度卖出一份。

**关键参数：** 网格间距、网格数量、价格上下限。

**所需数据：** 实时行情、历史波动区间

| 数据         | sec 现有         |
| ------------ | ---------------- |
| 实时行情     | ✓ `sec quote -r` |
| 历史波动区间 | ✓ kline 数据     |

### 5.2 市场中性 (Market Neutral)

同时做多一组股票、做空另一组（或做空指数期货），消除市场 Beta 暴露。

```text
组合收益 = α（选股能力）+ β×市场收益
中性组合：做多选股组合，做空等额指数，β≈0，纯α
```

**所需数据：** 股票 Beta、指数数据、做空工具

| 数据      | sec 现有                |
| --------- | ----------------------- |
| 股票 Beta | ✗ 需指数历史 + 回归计算 |
| 指数数据  | ✗ 需指数行情            |

---

## 六、sec 可立即实现的策略

基于现有数据（OHLCV + 财务数据），可立即实现以下策略的信号计算：

| 策略         | 复杂度 | 纯信号计算               |
| ------------ | ------ | ------------------------ |
| 双均线交叉   | 低     | 只需日线收盘价           |
| MACD         | 低     | 只需日线收盘价           |
| RSI 超买超卖 | 低     | 只需日线收盘价           |
| 布林带       | 低     | 需 OHLC                  |
| 海龟突破     | 中     | 需 OHLC (ATR 计算）      |
| 动量选股     | 中     | 需月线收益率             |
| 价值因子扫描 | 中     | 需 PE/PB/PS + 全市场数据 |
| 格雷厄姆筛选 | 低     | 只需 EPS+BVPS            |

## 七、可参考的命令形态

```shell
sec strategy ma 600036 --fast 5 --slow 20      # 双均线信号
sec strategy macd 600036                         # MACD 信号
sec strategy rsi 600036                          # RSI 超买超卖
sec strategy boll 600036                         # 布林带
sec strategy turtle 600036                       # 海龟突破
sec strategy graham --pe 15 --pb 1.5             # 格雷厄姆筛选器
```

---

## 参考

- Jegadeesh & Titman (1993). _Returns to Buying Winners and Selling Losers_
- Fama & French (1993). _Common Risk Factors in Stock and Bond Returns_
- Asness, et al. (2019). _Size Matters, If You Control Your Junk_
- Chan, E. (2013). _Algorithmic Trading: Winning Strategies and Their Rationale_

## 八、实现详情 (sec strategy 命令）

### 命令结构

```bash
sec strategy ma 600036 --fast 5 --slow 20     # 双均线
sec strategy macd 600036 -f 12 -s 26 -g 9     # MACD
sec strategy rsi 600036 -p 14                 # RSI
sec strategy boll 600036 -p 20 -k 2.0         # 布林带
```

别名：`sec st <subcommand> <code>`

### 架构

```shell
cmd/strategy/
├── strategy.go      # 父命令注册 + 共享类型 (Signal) + displayTable + fetchOHLCV
├── ma.go            # ComputeMA + sma + NewMACLI
├── macd.go          # ComputeMACD + ema + NewMACDCLI
├── rsi.go           # ComputeRSI + NewRSICLI
├── boll.go          # ComputeBollinger + NewBollCLI
└── strategy_test.go # 13 个单元测试
```

### 纯函数设计

每个策略的核心计算是纯函数，接受 OHLCV 数据 + 参数，返回信号列表 + 指标值表格，无 CLI 依赖：

```go
func ComputeMA(quotes []*Quote, fast, slow int) (headers []string, data [][]string, signals []Signal)
func ComputeMACD(quotes []*Quote, fast, slow, signal int) (headers []string, data [][]string, signals []Signal)
func ComputeRSI(quotes []*Quote, period int, overbought, oversold float64) (headers []string, data [][]string, signals []Signal)
func ComputeBollinger(quotes []*Quote, period int, k float64) (headers []string, data [][]string, signals []Signal)
```

### 输出格式

每个子命令输出：最近 20 个交易日的指标值表格 + 信号统计：

```shell
证券代码：SH600036  证券名称：招商银行  策略：双均线 (5,20)

  日期        收盘     MA5      MA20    信号
  2026-05-15   42.50   42.30    41.80    -
  2026-05-18   43.10   42.60    41.85   ☍ 金叉买入     ← 红色粗体
  2026-05-25   41.80   42.30    42.10   ☍ 死叉卖出     ← 绿色粗体

信号统计：买入 2 次 / 卖出 1 次
```

### 数据流

```
sec st ma 600036 -f 5 -s 20
  ↓
sina.Search → 确定交易所 + 纯数字代码
  ↓
eastmoney.GetQuoteHistory → []*Quote （最近 250 天）
  ↓
ComputeMA(quotes, 5, 20) → 计算 MA5/MA20 + 检测金叉死叉
  ↓
displayTable() → tablewriter 终端表格 （最近 20 行）
```

### 已实现 vs 待实现

| 策略         | 状态   | 命令           |
| ------------ | ------ | -------------- |
| 双均线       | ✓      | `sec st ma`    |
| MACD         | ✓      | `sec st macd`  |
| RSI          | ✓      | `sec st rsi`   |
| 布林带       | ✓      | `sec st boll`  |
| 海龟交易     | 待实现 | —              |
| K 线叠加显示 | 待实现 | `--chart` flag |
| 多因子扫描   | 待实现 | `sec st scan`  |
