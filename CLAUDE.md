# CLAUDE.md

> **核心规范**: 根目录 `AGENTS.md` 是本项目的唯一 SSOT。**禁止在此文件重复抄写构建命令、测试流程或架构说明**。

## 1. Claude Code CLI 交互行为规范
- **Non-Interactive Mode**: 任何 CLI 操作均优先附加非交互参数（如 `-y`, `--ci`, `--non-interactive`）
- **修改代码前**: 必须先输出 3 行以内简短 Plan，明确修改范围与验证步骤
- **修改后**: 自动执行 `make lint` + `make test` + `.\dev++.ps1 -WebOnly` 作为验证
- **响应风格**: 极致简炼，仅输出 `git diff` 摘要和关键命令，无通用编程概念解释
- **Shell 环境**: Windows 11 本地开发推荐 PowerShell (`.\dev++.ps1`)，Claude Code 运行时使用 Bash

## 2. 工具链与验证规则
- **首选 Shell**: PowerShell (Windows 11) / Bash (Claude Code)
- **验证命令序列**:
  1. `make lint`
  2. `make test`
  3. `.\dev++.ps1 -WebOnly`
- **自定义指令**: 未发现 MCP 或 Claude Code 专用 slash commands
- **文件排除**: 严禁读取/索引 `.omc/`, `.superpowers/`, `.cocoindex_code/`, `vendor/`, `*.exe`

## 3. 项目哲学约束
- 严格遵循 `AGENTS.md` 中的 **Strict Minimalist Philosophy**
- 任何新功能或架构调整必须与用户明确请求匹配
- 代码质量目标：80%+ 测试覆盖率，始终保持简洁与可维护性

> 此文件仅用于 Claude Code CLI 的行为规范，不应包含项目架构、构建细节或通用编程常识。