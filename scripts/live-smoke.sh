#!/usr/bin/env bash
# 对真实上游跑一遍各命令，检查接口和数据格式有没有变化。需要联网。
#
#   go build -o sec . && scripts/live-smoke.sh ./sec [结果目录]
#
# 每条命令失败后再试一次，两次都失败才算失败，避免偶发的网络问题误报；
# --json 命令还要求输出是非空的合法 JSON。结果写到结果目录的 results.tsv 和 summary.md，
# 有失败时退出 1。用临时 HOME，不读写本机的缓存和配置。
set -o pipefail

bin=$(cd "$(dirname "${1:?用法: live-smoke.sh <sec 可执行文件> [结果目录]}")" && pwd)/$(basename "$1")
out=${2:-$(mktemp -d)}
mkdir -p "$out/logs"
export HOME=$out/home
mkdir -p "$HOME/.sec"

commands=(
	"search 茅台 --json"
	"quote sh600519 --json"
	"quote 00700 --json"
	"quote hk00700 --json"
	"quote AAPL --json"
	"quote TCEHY --json"
	"info sh600519 --json"
	"info sh600519 -d --json"
	"info AAPL --json"
	"kline sh600519 --json"
	"kline 00700 --json"
	"kline AAPL --json"
	"quote-history sh600519 --json"
	"financials sh600519 --json"
	"financials AAPL --json"
	"valuation sh600519 --json"
	"valuation AAPL --json"
	"announcements sh600519 --json"
	"filings AAPL --json"
	"insider sh600519 --json"
	"ipo list --json"
	"bond --json"
	"bond-history --json"
	"metal --json"
	"commodity --json"
	"fx quote --json"
)
for m in capacity-utilization gdp industrial-production core-cpi core-pce cpi pce continued-claims earnings \
	initial-claims participation payrolls unemployment weekly-hours real-income real-pce equipment-investment \
	house-price housing housing-starts business-inventories corporate-profits new-orders bank-credit debt-service \
	lending-standards money effr target-rate treasury auctions debt debt-cash fiscal real-trade trade; do
	commands+=("macro us $m --json")
done
for m in gdp industrial nonmanufacturing-pmi pmi core-cpi cpi ppi unemployment household retail investment \
	property enterprise deposits financing loans money lpr reverse-repo reverse-repo-history rrr treasury fiscal \
	services-trade trade; do
	commands+=("macro cn $m --json")
done

# check 运行一次命令，成功时返回 0；--json 命令的输出必须是非空的合法 JSON。
check() {
	local log=$1
	shift
	timeout 90 "$bin" "$@" >"$log.out" 2>"$log.err" || return 1
	if [[ " $* " == *" --json "* ]]; then
		jq -e 'if type == "array" or type == "object" then length > 0 else true end' "$log.out" >/dev/null 2>>"$log.err" || {
			echo "输出不是非空的 JSON" >>"$log.err"
			return 1
		}
	fi
}

printf 'command\tstatus\tseconds\n' >"$out/results.tsv"
failed=() flaky=()
i=0
for c in "${commands[@]}"; do
	i=$((i + 1))
	log=$out/logs/$(printf %03d "$i")
	read -ra args <<<"$c"
	start=$SECONDS
	if check "$log" "${args[@]}"; then
		status=ok
	elif sleep 5 && check "$log" "${args[@]}"; then
		status=flaky
		flaky+=("$c")
	else
		status=failed
		failed+=("$c")
	fi
	printf '%s\t%s\t%s\n' "$c" "$status" $((SECONDS - start)) >>"$out/results.tsv"
	echo "[$status] sec $c"
done

{
	echo "## Live smoke test"
	echo
	echo "${#commands[@]} commands: $((${#commands[@]} - ${#failed[@]} - ${#flaky[@]})) ok, ${#flaky[@]} passed on retry, ${#failed[@]} failed."
	if ((${#failed[@]} > 0)); then
		echo
		echo "| Failed command | Error |"
		echo "| --- | --- |"
		i=0
		for c in "${commands[@]}"; do
			i=$((i + 1))
			for f in "${failed[@]}"; do
				if [[ $f == "$c" ]]; then
					err=$(grep -v ' WARN \| DEBUG ' "$out/logs/$(printf %03d "$i").err" | tail -1 | cut -c1-300 | tr '|' '/')
					echo "| \`sec $c\` | ${err:-（无错误输出）} |"
				fi
			done
		done
	fi
	if ((${#flaky[@]} > 0)); then
		echo
		echo "Passed on retry: $(printf '`sec %s` ' "${flaky[@]}")"
	fi
} >"$out/summary.md"
cat "$out/summary.md"

((${#failed[@]} == 0))
