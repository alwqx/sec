# sec watch — 自选组合

## 概述

`sec watch` 管理股票自选组合，数据持久化到 `~/.sec/watchlist.json`。支持添加/移除股票，默认展示自选列表的实时行情。

## 用法

```bash
# 查看自选组合（含实时行情）
sec watch

# 别名
sec w

# 添加股票（支持批量、支持代码或名称搜索）
sec watch add 600036
sec watch add 600036 000001 600519
sec watch add 招商银行

# 移除股票（支持纯代码或带前缀代码）
sec watch remove 600036
sec watch remove SH600036
```

## 数据存储

自选列表保存在 `~/.sec/watchlist.json`，JSON 格式：

```json
[
  {
    "code": "600036",
    "excode": "SH600036",
    "name": "招商银行",
    "added_at": "2026-06-05"
  }
]
```

按代码数字排序，同一只股票不会重复添加。

## 显示字段

| 列     | 说明                    | 数据来源     |
| ------ | ----------------------- | ------------ |
| 代码   | 交易所前缀 + 数字代码   | 自选列表     |
| 名称   | 证券简称                | 自选列表     |
| 现价   | 最新成交价              | 新浪实时行情 |
| 涨跌幅 | （现价-昨收）/昨收×100% | 计算得出     |
| 涨跌额 | 现价 - 昨收             | 计算得出     |
| 最高   | 当日最高价              | 新浪实时行情 |
| 最低   | 当日最低价              | 新浪实时行情 |

涨跌幅列红色粗体表示上涨，绿色粗体表示下跌。

## 架构

```shell
sec watch → 读取 ~/.sec/watchlist.json
              ↓
          sina.QueryQuoteList() → 批量获取实时行情
              ↓
          计算涨跌幅 → tablewriter 表格输出

sec watch add <code>
  → sina.Search() → 解析代码和名称
  → 追加到 watchlist.json（去重 + 排序）

sec watch remove <code>
  → 从 watchlist.json 中移除匹配项
```
