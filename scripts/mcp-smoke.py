#!/usr/bin/env python3
"""对 sec mcp 跑一遍 MCP 协议：握手、列出工具、调用工具，检查输出格式。需要联网。

    go build -o sec . && scripts/mcp-smoke.py ./sec
    scripts/mcp-smoke.py ./sec --call get_quote '{"codes": ["600519", "$AAPL"]}'

不带 --call 时：
  1. 握手并列出全部工具；
  2. 每个工具用 SAMPLES 里的示例参数调用一次，检查结果是统一信封（schema_version、tool、notes、data），
     并报告大小，超过 4 KB 时提醒精简；
  3. 每个有必填参数的工具用空参数调用一次，检查返回 isError 而不是协议错误。
有失败时退出 1。新增工具后在 SAMPLES 里加一条示例参数，否则这个工具只做空参数检查。

带 --call 时只调用一个工具，原样打印结果。只用标准库，不需要 npm 或 pip。
"""

import argparse
import json
import queue
import subprocess
import sys
import threading

# 每个工具的示例参数，尽量覆盖不同市场。新增工具时在这里加一条。
SAMPLES = {
    "search_security": {"query": "腾讯"},
    "get_quote": {"codes": ["600519", "00700", "$AAPL", "易方达蓝筹"]},
    "get_kline_summary": {"code": "SH000300"},
    "get_financials": {"code": "$AAPL", "period": "quarter"},
    "get_valuation": {"code": "000001"},
    "list_filings": {"code": "SH600036", "category": "annual", "limit": 5},
    "get_macro_series": {"indicator": "cn.cpi"},
}

# 单个工具结果的建议上限，见 docs/mcp.md 的「添加工具」。
RESULT_BUDGET = 4096
TIMEOUT = 60


class Server:
    """通过 stdin/stdout 和 sec mcp 收发 JSON-RPC，每条消息一行。"""

    def __init__(self, binary):
        self.proc = subprocess.Popen(
            [binary, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True
        )
        self.lines = queue.Queue()
        self.stderr = []
        self.next_id = 0
        threading.Thread(target=self._read, args=(self.proc.stdout, self.lines.put), daemon=True).start()
        threading.Thread(target=self._read, args=(self.proc.stderr, self.stderr.append), daemon=True).start()

    @staticmethod
    def _read(stream, sink):
        for line in stream:
            sink(line)

    def notify(self, method, params=None):
        self._send({"jsonrpc": "2.0", "method": method, **({"params": params} if params is not None else {})})

    def request(self, method, params=None):
        self.next_id += 1
        self._send({"jsonrpc": "2.0", "id": self.next_id, "method": method, **({"params": params} if params is not None else {})})
        while True:
            try:
                line = self.lines.get(timeout=TIMEOUT)
            except queue.Empty:
                raise RuntimeError(f"{method} 超过 {TIMEOUT} 秒没有响应") from None
            message = json.loads(line)
            # 只取本次请求的响应，跳过 server 发来的通知。
            if message.get("id") == self.next_id:
                return message

    def _send(self, message):
        self.proc.stdin.write(json.dumps(message, ensure_ascii=False) + "\n")
        self.proc.stdin.flush()

    def close(self):
        self.proc.stdin.close()
        try:
            return self.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            return None


def initialize(server):
    response = server.request(
        "initialize",
        {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "mcp-smoke", "version": "1"}},
    )
    if "error" in response:
        raise RuntimeError(f"握手失败：{response['error']}")
    server.notify("notifications/initialized")
    return response["result"]


def call_tool(server, name, arguments):
    """返回 (是否 isError, 文本)。协议错误抛异常。"""
    response = server.request("tools/call", {"name": name, "arguments": arguments})
    if "error" in response:
        raise RuntimeError(f"协议错误：{response['error']}")
    result = response["result"]
    text = "".join(item.get("text", "") for item in result.get("content", []))
    return bool(result.get("isError")), text


def check_envelope(name, text):
    """检查统一信封，返回问题列表。"""
    try:
        out = json.loads(text)
    except json.JSONDecodeError as err:
        return [f"结果不是合法 JSON：{err}"]
    problems = []
    for field in ("schema_version", "tool", "source", "notes", "data"):
        if field not in out:
            problems.append(f"缺少字段 {field}")
    if out.get("tool") != name:
        problems.append(f"tool 是 {out.get('tool')!r}，应为 {name!r}")
    if not isinstance(out.get("notes"), list):
        problems.append("notes 应为数组")
    return problems


def smoke(server):
    failures = 0
    info = initialize(server)
    print(f"server：{info['serverInfo']['name']} {info['serverInfo']['version']}，协议 {info['protocolVersion']}")

    tools = server.request("tools/list")["result"]["tools"]
    print(f"工具：{len(tools)} 个")
    for tool in tools:
        print(f"  {tool['name']}：{tool.get('description', '').split('。')[0]}")

    for tool in tools:
        name = tool["name"]
        print(f"\n== {name}")
        sample = SAMPLES.get(name)
        if sample is None:
            print("  跳过示例调用：SAMPLES 里没有这个工具的示例参数")
        else:
            is_error, text = call_tool(server, name, sample)
            if is_error:
                print(f"  失败  示例调用返回 isError：{text}")
                failures += 1
            else:
                problems = check_envelope(name, text)
                size = len(text.encode())
                out = json.loads(text) if not problems else {}
                status = "失败" if problems else "通过"
                print(f"  {status}  示例调用 {json.dumps(sample, ensure_ascii=False)}，{size} 字节，as_of={out.get('as_of')!r}")
                for note in out.get("notes", []):
                    print(f"        note：{note}")
                for problem in problems:
                    print(f"        {problem}")
                if size > RESULT_BUDGET:
                    print(f"  提醒  结果超过 {RESULT_BUDGET} 字节，考虑精简")
                failures += bool(problems)

        if tool.get("inputSchema", {}).get("required"):
            is_error, text = call_tool(server, name, {})
            if is_error:
                print(f"  通过  空参数返回 isError：{text}")
            else:
                print("  失败  空参数没有返回 isError")
                failures += 1
    return failures


def main():
    parser = argparse.ArgumentParser(description="对 sec mcp 跑一遍 MCP 协议并检查输出格式。")
    parser.add_argument("binary", help="sec 可执行文件，如 ./sec")
    parser.add_argument("--call", nargs=2, metavar=("TOOL", "JSON"), help="只调用一个工具，参数是 JSON 对象")
    args = parser.parse_args()

    server = Server(args.binary)
    try:
        if args.call:
            name, raw = args.call
            initialize(server)
            is_error, text = call_tool(server, name, json.loads(raw))
            try:
                text = json.dumps(json.loads(text), ensure_ascii=False, indent=2)
            except json.JSONDecodeError:
                pass
            print(("isError：" if is_error else "") + text)
            failures = int(is_error)
        else:
            failures = smoke(server)
    except (RuntimeError, OSError, json.JSONDecodeError) as err:
        print(f"失败：{err}", file=sys.stderr)
        failures = 1
    code = server.close()

    if server.stderr:
        print("\nsec mcp 的 stderr：", file=sys.stderr)
        sys.stderr.write("".join(server.stderr))
    if code != 0:
        print(f"sec mcp 退出码 {code}（None 表示超时后被强制结束）", file=sys.stderr)
        failures += 1
    if not args.call:
        print(f"\n{'全部通过' if failures == 0 else f'{failures} 项失败'}")
    sys.exit(1 if failures else 0)


if __name__ == "__main__":
    main()
