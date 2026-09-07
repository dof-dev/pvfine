# AGENTS.md

## 项目协作约束

- 始终使用简体中文回复。
- 不得泄露 secrets，包括 `.env`、key、token、credentials、password 等内容。
- 新增功能尽量放在已有功能之后，保持改动范围清晰。
- 如果无法确定用户意图，先确认需求再修改代码。
- 如果发现未确认的代码改动，以最新代码为准，不直接覆盖。
- 管理 Python 依赖时，如果项目目录存在 `uv.lock`，使用 `uv` 管理依赖。
- 遇到 Node.js 版本问题时，可使用 `fnm` 切换版本。

## 验证约定

- Go 代码修改后运行相关 `go test`，必要时运行 `go test ./...`。
- 前端代码修改后在 `frontend` 目录运行 `npm run build`。
- 提交前确认本地归档文件 `*.pvf`、依赖目录和构建产物没有被加入 Git。
