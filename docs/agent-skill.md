# 在 AI agent 里使用 sec

[`skills/sec/SKILL.md`](../skills/sec/SKILL.md) 是一份 Agent Skill，告诉 Claude Code、Codex 等 AI 编程助手怎样调用 sec 命令行取数，并按数据如实回答。装好后直接用自然语言提问，比如「茅台现在估值贵吗」「美国 9 月非农是多少」，agent 会自己运行 `sec ... --json` 并解释结果。

这份 Skill 只是使用说明，不改变 sec 本身：数据仍然由本机的 `sec` 命令获取，模型由你使用的 agent 提供。设计背景见 [AI/LLM 接入设计](ai.md)。

## 前提

- 已安装 `sec` 并在 `PATH` 里，`sec --version` 能运行。安装方法见 [README](../README.md#安装)。
- 查美股 SEC 数据前设置联系邮箱：`sec config set sec-edgar-email you@example.com`。

## 安装

Skill 是一个目录，里面放 `SKILL.md`。

### Claude Code

所有项目都可用（个人级）：

```bash
mkdir -p ~/.claude/skills/sec
curl -fsSL https://raw.githubusercontent.com/alwqx/sec/main/skills/sec/SKILL.md -o ~/.claude/skills/sec/SKILL.md
```

只在某个项目里用时，放到该项目的 `.claude/skills/sec/SKILL.md`。

### Codex

```bash
mkdir -p ~/.codex/skills/sec
curl -fsSL https://raw.githubusercontent.com/alwqx/sec/main/skills/sec/SKILL.md -o ~/.codex/skills/sec/SKILL.md
```

### 其他工具

支持 Agent Skills（`SKILL.md`）格式的工具，放到它的 skills 目录即可。不支持的工具（比如只有项目规则或自定义指令的），可以把 `SKILL.md` 中 `---` 之后的正文复制到项目规则或系统提示里。

## Skill 要求 agent 做什么

- 只用 sec 输出的数字，不凭记忆补数据；sec 没有的数值直接说没有。
- 一律用 `--json`（宏观数据用 `--standard-json`），用 `jq` 只取需要的字段，避免把很长的估值序列、K 线读进上下文。
- 保留 `_note` 口径说明、推导值标记 `*` 和数据日期；`null` 不当作 0。
- 不给买卖建议；估值的「偏低区间、合理区间、偏高区间」说明是规则判定，并带上免责声明。

Skill 只是给模型的说明，模型不一定每次都遵守。涉及决策的数字请对照 sec 的原始输出核对。

## 更新

Skill 随 sec 的命令变化更新。升级 sec 后重新运行上面的 `curl` 命令即可。
