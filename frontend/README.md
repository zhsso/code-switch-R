# Codex++ WebUI

Vue 3、TypeScript 和 Vite 构成的 Codex 转发管理界面。后端调用统一封装在 `src/services/`，底层通过 `src/runtime.ts` 使用 HTTP RPC 和 Server-Sent Events；生产交付只通过仓库根目录的 Docker Compose 构建。

```bash
npm ci
npm run dev
npm run build
```

开发服务器默认监听 `127.0.0.1:9245`，并将 `/api` 代理到 Go 服务的 `127.0.0.1:8080`。`npm run build` 会先运行 `vue-tsc`，再生成生产资源。不要提交 `dist/` 或 `node_modules/`。
