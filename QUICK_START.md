# Codex++ 快速上手

```bash
docker compose up --build -d
docker compose ps
```

容器健康后访问 `http://127.0.0.1:8080`，在首页添加至少一个支持 OpenAI Responses API 的供应商。默认上游路径是 `/responses`，默认认证方式是 Bearer。

然后手动编辑宿主 `~/.codex/config.toml`：

```toml
model = "gpt-5.6-sol"
model_provider = "codex_plus"

[model_providers.codex_plus]
name = "Codex++"
base_url = "http://127.0.0.1:18100"
wire_api = "responses"
```

Codex++ 不会挂载或修改宿主 `.codex` 目录。发起一次 Codex 请求后，可在 WebUI 的日志页确认实际供应商、状态码、Token 和费用。

停止服务但保留命名卷数据：

```bash
docker compose down
```
