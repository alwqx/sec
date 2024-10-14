# sec

security info CLI

![](https://github.com/alwqx/picx-images-hosting/raw/master/common/github/sec_quote.3k7zp33ifg.gif)

## 命令

```shell
$ sec --help
Secutiry Information Client

Usage:
  sec [flags]
  sec [command]

Available Commands:
  help        Help about any command
  info        Print basic information of a secutiry/stock
  quote       Secutiry quote root Command
  search      Search code and name of a secutiry/stock

Flags:
  -D, --debug     Enable debug mode
  -h, --help      help for sec
  -v, --version   Show version information

Use "sec [command] --help" for more information about a command.
```

1. 搜索证券
   ```shell
   $ sec search lxzk
   证券代码 	证券名称 	证券类型 	交易所
   SH688047	龙芯中科	stock   	sh
   SZ300112	万讯自控	stock   	sz
   HK02186 	绿叶制药	stock   	hk
   ```
2. 查看证券基本信息
   ```shell
   $ sec info lxzk
   证券代码	SH688047
   简称历史	龙芯中科
   公司名称	龙芯中科技术股份有限公司
   上市日期	2022-06-24
   发行价格	60.06
   行业分类	半导体
   主营业务	处理器及配套芯片的研制、销售及服务
   办公地址	北京市海淀区中关村环保科技示范园区龙芯产业园 2 号楼
   公司网址	http://www.loongson.cn
   当前价格	119.62
   市净率 PB	14.47
   市盈率 TTM	0.00
   总市值  	479.68 亿
   流通市值	334.51 亿
   ```
3. 查看行情信息
   ```shell
   $ sec quote lxzk
   时间                	当前价格   	昨收  	今开 	最高   	最低  	成交量   	成交额 	名称     	证券代码
   2024-09-30 15:00:01	119.62 20%	99.68	106 	119.62	104.5	825.67 万	9.38 亿	龙芯中科	SH688047
   $ sec quote lxzk,lxjm,SH600036
   时间                	当前价格   	昨收  	今开  	最高   	最低  	成交量   	成交额  	名称     	证券代码
   2024-09-30 15:00:00	43.46 7.4%	40.48	42   	43.95 	41.01	1.60 亿  	68.42 亿	立讯精密	SZ002475
   2024-09-30 15:00:01	119.62 20%	99.68	106  	119.62	104.5	825.67 万	9.38 亿 	龙芯中科	SH688047
   2024-09-30 15:00:00	37.61 5.6%	35.63	36.35	38    	35.92	2.56 亿  	94.43 亿	招商银行	SH600036
   ```

## 安装

进入 [releases](https://github.com/alwqx/sec/releases) 页面，下载指定操作系统的二进制文件。

Mac/Linux 要把二进制文件放在 `PATH` 路径下。

Windows 要把二进制文件放在 `系统环境变量` 下。

## 开发计划

- [ ] 基本信息支持打印股东结构
- [ ] 行情信息打印对齐
- [ ] 行情信息实时更新
- [ ] 行情信息蜡烛图

## 致谢

- [akshare](https://github.com/akfamily/akshare)
- [rains](https://github.com/rookie0/rains)
