# sec change log

### v0.2.7

1. `quote-history` 命令添加选项 `begin`、`end`
2. `quote-history` 命令添加选项 `fqt` 复权类型，选项为
   - bfq 不复权
   - qfq 前复权
   - hfq 后复权

### v0.2.6

1. 修复 github workflow 权限
2. 修复 go.mod 中 `golang.org/x/net` dependabot 版本告警

### v0.2.5

`quote-history` 命令改进：

1. 输出中，将`涨跌幅`、`涨跌额` 合并到收盘价
2. 证券代码添加交易所前缀
3. 多个行情信息，按照时间`升序`、`降序`排列

### v0.2.4

1. 拆出 `utils`
2. `quote-history` 命令初始化

### v0.2.3

1. 删除 debug 日志
2. 添加短命令 `info->i` `quote-q` `search-s`
3. 修复 info 等查询美股 panic 问题

### v0.2.2

1. 修复 A 股只判断上交所，没有判断深交所和北京交所的问题

### v0.2.1

1. 更新版本信息

### v0.2.0

1. sina 搜索接口返回数据新增一个字段，支持该变化
2. `info`/`quote` 支持港股

### v0.1.4

1. 股票信息打印价格变化

### v0.1.3

1. 全部命令添加 `--debug`/`-D` flag
2. quote 命令最多支持查询 5 个证券
3. quote 支持实时 (3s) 更新行情信息

### v0.1.2

1. 修复行情命令输出格式不稳定
2. 对多个证券进行排序

### v0.1.1

1. 添加 github actions

### v0.1.0

1. quote 拆分成子命令
