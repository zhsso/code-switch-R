# Repository Guidelines

## Project Structure & Module Organization

本仓库是面向 Linux 的 Codex Responses API 转发服务与 WebUI，仅通过 Docker Compose 交付，不包含 Wails、桌面应用、托盘或前端 bindings 生成流程。

`main.go` 负责注册服务并控制启动、关闭顺序；涉及数据库、全局数据库写入队列、代理服务、健康检查或后台任务时，不要随意调整初始化和关闭顺序。Go 业务代码集中在 `services/`，同目录放置 `*_test.go`；根目录的 Go 文件负责服务注册、HTTP/RPC、配置、迁移和 WebUI 静态资源。前端在 `frontend/src/`，模型价格数据和相关测试在 `resources/model-pricing/`。`Dockerfile`、`compose.yaml` 和 `Taskfile.yml` 定义构建、运行和验证流程。

生产镜像将前端构建结果嵌入 Go 二进制，容器使用命名卷 `code-switch-R_codeswitch-data` 保存 `/data` 下的数据库和应用数据。不要把本地数据库、日志、密钥或生成的 `frontend/dist/`、`frontend/node_modules/` 加入提交。

## Build, Test, and Development Commands

集成运行和重建镜像使用 Docker Compose：

```bash
docker compose up --build -d
docker compose ps
docker compose logs -f codeswitch
docker compose down
```

修改 Go 或前端后，必须使用 `docker compose up --build -d` 重建并重启容器；单独执行 `docker compose restart` 不会把新代码编入镜像。配置校验和本地构建：

```bash
docker compose config --quiet
docker compose build
go test ./...
go vet ./...
cd frontend && npm ci
cd frontend && npm run build
```

前端开发服务器使用 Vite，默认监听 `127.0.0.1:9245`，并将 `/api` 代理到 Go 服务的 `127.0.0.1:8080`：

```bash
cd frontend && npm run dev
```

需要热更新前端时，另行启动本地 Go 服务（例如 `go run .`）；只验证完整交付链路时，优先使用 Docker Compose。`Taskfile.yml` 提供 `task up`、`task down`、`task logs`、`task status` 和 `task test` 的快捷入口，但这些任务最终仍调用 Docker Compose、Go 和 npm。

## Coding Style & Naming Conventions

Go 代码使用 `gofmt`，保持包名短小、服务类型以 `Service` 结尾，测试函数使用 `TestXxx`。Vue 单文件组件使用 PascalCase 文件名，组合式函数使用 `useXxx`，普通 TypeScript 工具函数使用 camelCase。前端调用后端能力时优先封装在 `frontend/src/services/`，避免组件直接堆叠 HTTP/RPC 调用细节。

后端导出 RPC 方法由 `main.go` 中的 registry 显式注册，不要添加已经移除的 Wails bindings 或生成步骤。修改 RPC 契约时同步更新对应的前端 service 类型和调用代码，并补充必要的错误处理。

## Testing Guidelines

优先补与改动同包的 Go 测试。代理路由、流式响应、供应商配置、模型容量降级、价格计算、数据库迁移或写入队列等共享逻辑变更后，运行 `go test ./...` 和 `go vet ./...`。前端 UI 或类型改动后运行 `cd frontend && npm run build`；Dockerfile 或 Compose 改动后运行 `docker compose config --quiet` 和 `docker compose build`。

测试夹具放在 `services/testdata/` 或对应包的明确测试目录，不要把临时实验文件放入正式测试集合。涉及流式转发时，同时覆盖客户端断开、上游中途断流、容量/过载错误、备用 Provider 降级和已经提交响应后的不可降级边界。

## Commit & Pull Request Guidelines

提交信息使用 Conventional Commits，例如 `feat: 完善定价规则`、`fix: 修复流式降级`、`chore: 清理测试产物`。PR 需说明变更范围、验证命令和 Docker/配置影响；UI 变化附截图。

不要提交 `*.exe`、`test-results-*.json`、`nul`、`%s`、`frontend/dist/`、`frontend/node_modules/`、本地数据库、日志、Docker 数据卷内容或密钥配置，也不要添加生成署名或共同作者尾注。
