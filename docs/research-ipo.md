# IPO / 上市信息功能调研

> 目标：在中国大陆（沪深京）和香港证券市场提供「上市公司列表、上市时间、招股说明书」信息的查询能力。
> 状态：调研阶段（2026-07-02）

---

## 一、官方数据源

### 1.1 中国大陆（A 股）

| 交易所 | 披露入口 | 提供内容 |
|--------|----------|----------|
| 上海证券交易所（SSE） | `https://www.sse.com.cn/ipo/` | 审核进展、预披露、招股书（科创板：`https://kcb.sse.com.cn/xqsj/`） |
| 深圳证券交易所（SZSE） | `https://www.szse.cn/ipo/` | 审核进展、招股书申报稿（最终链接走巨潮） |
| 北京证券交易所（BSE） | `https://www.bse.cn/disclosure/info.html` | IPO 审核状态、招股书、上市公司数据 |
| **巨潮资讯（cninfo）** | `https://www.cninfo.com.cn` | ⭐ 证监会指定信息披露平台，沪深京三地 IPO 公告、定稿招股书 PDF |

### 1.2 香港（HKEX）

| 入口 | 提供内容 |
|------|----------|
| `https://www.hkexnews.hk` | PDF 下载：招股书、聆讯后资料集、上市通告 |
| `https://www3.hkexnews.hk/hyperlink/hyperlist.HTM` | 每日上市公司清单（CSV/Excel，含代码、名称、板块、上市日期） |
| `https://www.hkex.com.hk/Listing/Listing-Information/Main-Board-IPO-and-New-Listing?sc_lang=en` | 待上市/已审批 IPO 日历 |
| `https://www1.hkexnews.hk/search/titlesearch.xhtml` | 招股书文档检索入口 |

---

## 二、第三方数据 API / SDK

### 2.1 推荐入口

| 数据源 | 覆盖 | 招股书链接 | 获取方式 | 是否收费 |
|--------|------|-----------|----------|----------|
| **巨潮资讯 API**（被 AKShare 等封装） | SSE/SZSE/BSE | ✅（详情页 → PDF） | 非官方 POST JSON API | 免费 |
| **AKShare** `stock_zh_a_disclosure_report_cninfo()` | SSE/SZSE/BSE | ✅ | Python 封装巨潮 API | 免费 |
| **东方财富 PUSH2** `fid=f26` | SSE/SZSE/BSE | ❌（仅跳转链接） | 公开 REST JSON | 免费 |
| **AKShare** `stock_new_ipo_em()` | SSE/SZSE/BSE | ❌ | 东方财富新股接口 | 免费 |
| **Tushare Pro** `new_share()` | SSE/SZSE（BSE 部分） | ❌ | Token + 积分 | 部分付费 |
| **HKEX hyperlist.csv** | HK | ❌ | 直链下载 | 免费 |
| **HKEX News Search** | HK | ✅（PDF） | HTML 抓取 | 免费 |

> 除巨潮资讯外，免费渠道**极少直接给出 PDF 下载直链**——多数给出公告详情页 URL，再二次解析拿到 PDF。

### 2.2 巨潮资讯 API（cninfo）详细剖析

这是综合性价比最高的方案，本项目 `provider/cninfo/` 已调用过其公告接口。

```
POST http://www.cninfo.com.cn/new/hisAnnouncement/query
Content-Type: application/x-www-form-urlencoded

pageNum=1&pageSize=30
column=szse                # szse | sse | SSE | (空可查)
stock=&searchkey=
category=category_sf_szsh  # 首次公开发行及上市
seDate=2024-01-01~2024-12-31
sortName=&sortType=
isHLtitle=true
```

```json
{
  "totalAnnouncement": 1234,
  "announcements": [
    {
      "announcementId": "12345678",
      "secCode": "300001",
      "secName": "特锐德",
      "announcementTitle": "首次公开发行股票并在创业板上市招股说明书",
      "storageTime": "2024-06-01 00:00:00",
      "adjunctUrl": "/new/disclosure/detail?stockCode=300001&announcementId=12345678&orgId=990000xxx"
    }
  ]
}
```

- 详情页访问 `cninfo.com.cn/new/disclosure/detail?...` 即可获得 PDF 下载入口。
- `category` 字段对照表（cninfo 官方分类编码）：

| 分类编码 | 含义 |
|----------|------|
| `category_ndbg_szsh` | 年度报告 |
| `category_sf_szsh` | **首次公开发行及上市（IPO）** |
| `category_scgkfx_szsh` | 招股发行上市（再融资类） |
- 频率限制：非正式 API，建议 3–5 秒/请求，并走本地缓存。

### 2.3 东方财富 PUSH2（新股列表）

```
# 新股列表
GET https://push2.eastmoney.com/api/qt/clist/get
  ?fid=f26                 # 按上市时间排序
  &fs=m:0+t:80+t:81+s:2048 # 沪深北交所 + 科创板 + 创业板
  &po=1&pz=50&pn=1

# 新股日历（近期 IPO 排期）
GET https://push2ex.eastmoney.com/api/qt/newcalendar/get
  ?source=newstock&client=app&start_date=YYYY-MM-DD&end_date=YYYY-MM-DD

# 单股查询（代码 + 名称 + 上市时间）
GET https://push2.eastmoney.com/api/qt/stock/get
  ?secid=0.300001&fields=f57,f58,f116,f117,f169,f170,f171,f26
```

主要字段：`f57`=代码、`f58`=名称、`f26`=上市日期、`f43`=发行价、`f44`=市盈率。
东方财富优势是**价格 / PE / 中签率**，缺点是无 PDF 直链。

### 2.4 HKEX

```text
# 全量上市公司清单（每日更新）
GET https://www3.hkexnews.hk/hyperlink/hyperlist.HTM   → 实际重定向到 .csv 下载

# 招股书搜索（表单提交）
POST https://www1.hkexnews.hk/search/searchindex.xhtml
  lang=ZH
  category=0
  from=20240101&to=20241231
  stockId=00700
  title=
  t1code=40000            # 主板新上市 / 招股书
  &searchType=1
```

> HKEX 在 2024–2025 年已将 `searchnew.aspx` 等旧端点下线，迁移到 `searchindex.xhtml`。抓取脚本较脆弱，建议每月重新校验一次选择器。

---

## 三、A 股实现路线（推荐）

```text
┌─────────────────────────────────────────────────────────┐
│  IPO 元数据 │ 上市公司列表 │ 上市日历 │ 即将上市      │
└──────┬──────────────────────────────────────────────────┘
       │
       ▼
┌────────────────────┐     ┌────────────────────────────────┐
│ 东方财富 PUSH2     │────▶│  代码、名称、上市日、发行价、PE │
│（push2.eastmoney）  │     │  pz<=300 分页即可              │
└────────────────────┘     └────────────────────────────────┘
       │
       ▼（需要 PDF 时）
┌─────────────────────────────────────────┐
│ 巨潮资讯 cninfo                          │
│ POST /new/hisAnnouncement/query         │
│ category=category_sf_szsh               │
│ → 公告列表 → 详情页 → PDF 直链          │
└─────────────────────────────────────────┘
```

| 步骤 | 数据源 | Go 方法 | 返回 |
|------|--------|---------|------|
| `List()` — 已上市公司 | 东方财富 push2 `fid=f26` | `provider/eastmoney/ipo.go` | 代码、名称、上市日期、发行价、PE、首日涨跌幅 |
| `Calendar()` — 新股日历 | 东方财富 push2ex newcalendar | `provider/eastmoney/calendar.go` | 申购日、缴款日、预计上市日 |
| `Prospectus()` — 招股书 | 巨潮 cninfo `category_sf_szsh` | `provider/cninfo/prospectus.go` | 公告标题、日期、PDF URL |

---

## 四、港股实现路线

| 步骤 | 数据源 | Go 方法 | 返回 |
|------|--------|---------|------|
| `List()` — 全量上市公司 | `hkexnews.hk` hyperlist CSV | `provider/hkex/list.go` | 代码、名称、板块、ISIN、上市日期 |
| `Prospectus()` — 招股书 | HKEX 标题搜索 `t1code=40000` | `provider/hkex/prospectus.go` | 公告标题、日期、PDF URL |

> HKEX 较脆弱：CSV 直链稳定可用作「已上市公司」的权威源；招股书抓取需配合 cookie 会话 + HTML 解析，适合低频调用（每日/每周缓存）。

---

## 五、Go 复用性 / 已有基础设施

本项目（sec CLI）已具备的基本能力可直接复用：

| 组件 | 位置 | 用途 |
|------|------|------|
| `utils.MakeRequest` | `utils/http.go` | 通用 HTTP GET/POST、重试、UA、超时 |
| `GBK/GB18030 → UTF-8` | `utils/encoding.go` | 巨潮/东方财富部分页面为 GBK |
| `provider/cninfo/` | 已存在 | 调用过 `query` 公告接口，可直接增 IPO/招股书函数 |
| `provider/eastmoney/` | 已存在 | kline / quote-history 的 push2 模式可直接拷贝 |
| `tablewriter` + 着色 | 现有命令统一用法 | IPO 列表、日历渲染 |

推荐落地路径：

1. `provider/eastmoney/ipo.go` — 上市公司列表 + 新股日历（纯 REST JSON，最稳）
2. `provider/eastmoney/calendar.go`（或合入 ipo.go）— 新股日历
3. `provider/cninfo/prospectus.go` — 巨潮 IPO 公告 + 详情页 → PDF 链接
4. `provider/hkex/` — `list.go`（超链 CSV）+ `prospectus.go`（标题搜索抓取）
5. `cmd/ipo/ipo.go` — cobra 子命令，`List/Prospectus/Calendar` 三个子命令
6. `cmd/cmd.go` — 注册 `ipoCmd`

---

## 六、风险与注意事项

| 风险 | 缓解 |
|------|------|
| 巨潮 cninfo 为非官方接口，字段可能调整 | 封装在 `provider/cninfo/`，添加 ETag/缓存，失败回退 |
| 东方财富 PUSH2 字段 ID 是密文（f26 等） | 写死常量 + 字段映射；单测覆盖 |
| HKEX 频繁重构前端（最近一次 2024-02） | prospectus 仅每日抓取一次；列为主观功能而非 SLA |
| 巨潮频率触警（约 3 秒/次） | 本地 TTL 缓存（24h），列表类命令默认读缓存 |
| GBK 编码（老站点） | 复用 `utils.SimplifiedChinese` |
| BSE（北交所）上市公司数较少（~250） | 东方财富已含，cninfo `column=sse` 同样命中北交所 |

---

## 七、最终建议

1. **MVP（1 周内可期）**：东方财富 PUSH2 + 巨潮 cninfo 提供「沪深北交所 IPO 列表 + 配套招股书」，只读缓存 + 命令行表格渲染。
2. **扩展（2 周）**：HKEX hyperlist CSV 做港股上市公司清单；HKEX 标题搜索做招股书（半脆弱，仅低频调用）。
3. **可选升级**：付费 Tushare Pro（¥2,000/年）提供稳定 SLA，覆盖历史 IPO 全量档案。
4. **暂不做**：付费数据终端（Wind/Bloomberg）与纯网页爬取（SSE/SZSE 各自披露页）。

---

## 八、参考链接

- [AKShare IPO 数据文档](https://akshare.akfamily.xyz/data/ipo/ipo.html)
- [巨潮资讯 (cninfo.com.cn)](https://www.cninfo.com.cn)
- [深证信 API 文档](http://webapi.cninfo.com.cn/#/apiDoc)
- [东方财富新股数据](https://data.eastmoney.com/xg/xg/default.html)
- [SSE IPO 披露](https://ipo.sse.com.cn/renewal/)
- [SZSE IPO 披露](http://www.szse.cn/ipo/)
- [HKEX News Disclosure](https://www.hkexnews.hk)
- [HKEX 上市公司超链清单](https://www3.hkexnews.hk/hyperlink/hyperlist.HTM)
- [HKEX 上市日历](https://www.hkex.com.hk/Listing/Listing-Information/Main-Board-IPO-and-New-Listing)
- [Tushare Pro](https://tushare.pro)
