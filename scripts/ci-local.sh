#!/usr/bin/env bash
# 在本机按 CI 的步骤检查一遍，push 前自检，不用等 GitHub 的 runner。
#
#   scripts/ci-local.sh          # gofmt、go vet、go test、前端构建和测试
#   scripts/ci-local.sh --vuln   # 另外跑 govulncheck，需要联网
#
# 在 macOS 上还会用 cgo 编译 Desktop 的 webview 入口，对应 CI 里的 macOS 编译。
# 前端依赖缺失或 package-lock.json 更新过时才执行 npm ci；npm 缓存有问题时可设置 npm_config_cache 换一个缓存目录。
# 任一步失败即退出 1。
set -euo pipefail

vuln=false
for arg in "$@"; do
	case $arg in
	--vuln) vuln=true ;;
	-h | --help)
		sed -n '2,9p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "未知参数：${arg}（用法见 --help）" >&2
		exit 2
		;;
	esac
done

cd "$(dirname "$0")/.."
start=$SECONDS
step() { printf '\n==> %s\n' "$*"; }

step "gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "$unformatted"
	echo "以上文件需要 gofmt -w" >&2
	exit 1
fi

step "go vet ./..."
go vet ./...

step "go test ./..."
go test ./...

step "前端构建和测试（desktop/ui）"
(
	cd desktop/ui
	if [ ! -d node_modules ] || [ package-lock.json -nt node_modules ]; then
		npm ci
	fi
	npm run build
	npm test
)

if [ "$(uname -s)" = Darwin ]; then
	step "Desktop macOS 编译（cgo + webview）"
	bin=$(mktemp -d)/sec-desktop
	CGO_ENABLED=1 go build -o "$bin" ./desktop
	rm -f "$bin"
fi

if $vuln; then
	step "govulncheck ./..."
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
fi

printf '\n全部通过，用时 %ds\n' $((SECONDS - start))
