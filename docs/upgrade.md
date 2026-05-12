# 自动升级

`sec upgrade` 命令自动从 GitHub Releases 下载并安装最新版本。

## 用法

```
sec upgrade
```

## 选项

| 选项 | 说明 |
|------|------|
| `-D, --debug` | 开启 debug 日志 |

## 工作流程

1. 获取当前二进制中的版本号（构建时通过 `-ldflags` 注入 `version.Version`）
2. 调用 GitHub API `GET /repos/alwqx/sec/releases/latest` 获取最新 release
3. 比较本地版本与远端 `tag_name`，一致则输出 "Already up to date." 并退出
4. 不一致时，根据当前 OS 和 CPU 架构匹配对应的 release asset：
   - `sec-{version}-darwin-amd64.tar.gz`
   - `sec-{version}-darwin-arm64.tar.gz`
   - `sec-{version}-linux-amd64.tar.gz`
   - `sec-{version}-linux-arm64.tar.gz`
   - `sec-{version}-windows-amd64.zip`
5. 下载匹配的 archive
6. 解压提取 `sec`（或 `sec.exe`）二进制
7. 替换当前运行的二进制文件

## 平台支持

| OS | Arch | Archive |
|----|------|---------|
| macOS (darwin) | amd64 / arm64 | `.tar.gz` |
| Linux | amd64 / arm64 | `.tar.gz` |
| Windows | amd64 | `.zip` |

## 二进制替换策略

**Linux / macOS**：将新二进制写入同目录临时文件（`.sec-new-*`），`chmod 755` 后 `os.Rename` 原子替换。跨设备时回退到 copy 方式。

**Windows**：因运行中的 `.exe` 无法被覆盖，采用延迟替换策略：
1. 将新二进制写入 `sec.exe.new`
2. 创建 `sec.exe.upgrade.bat` 批处理脚本
3. 用户下次启动 `sec` 前，脚本会自动完成替换

## 示例

```bash
# 从 dev 版本升级到最新 release
$ sec upgrade
Current version: (dev)
Checking latest release...
Latest  version: v0.2.14
Downloading sec-v0.2.14-darwin-arm64.tar.gz (6396163 bytes)...
Upgraded to v0.2.14 successfully.

# 已是最新版本
$ sec upgrade
Current version: v0.2.14
Checking latest release...
Latest  version: v0.2.14
Already up to date.
```

## 实现

```
cmd/upgrade/upgrade.go    -- CLI 命令，包含下载、解压、替换全部逻辑
```

- 不使用第三方依赖，全部使用 Go 标准库（`archive/tar`、`archive/zip`、`compress/gzip`、`net/http`）
- GitHub API 无需认证（公开仓库，60 次/小时足够人工升级使用）
