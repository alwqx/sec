// Package main 实现 sec 桌面版（Desktop）。
//
// 桌面版复用 provider/ 数据层，通过本地 HTTP JSON API 向 WebView 内嵌的 React
// 前端提供数据。除本文件外，其余源文件均带 //go:build windows || darwin 构建
// 标签：在 Linux 上本包退化为仅含文档的空包，避免 CI 因「build constraints
// exclude all Go files」失败。
package main
