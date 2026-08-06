# Codex++

Codex++ 是面向 Linux 的 Codex Responses API 转发服务与 WebUI。它管理 OpenAI Responses 兼容供应商、路由与故障切换、健康检查、黑名单、请求统计、费用和按需完整抓包。项目仅以本地构建的 Docker Compose 服务交付，不包含桌面应用、托盘、Wails 或其他 CLI 平台集成。

WebUI 固定绑定宿主回环地址 `http://127.0.0.1:8080`，Codex relay 固定绑定 `http://127.0.0.1:18100`。Compose 使用 host 网络，但服务不会监听公网地址，也不提供登录认证，因此不要另行转发或暴露这些端口。

## 启动

要求 Linux、Docker Engine 与 Docker Compose v2。镜像只在本机为 `linux/amd64` 构建，不会拉取或发布 Codex++ 成品镜像。

```bash
docker compose up --build -d
docker compose ps
```

容器健康后打开 `http://127.0.0.1:8080`。查看日志或停止服务：

```bash
docker compose logs -f codeswitch
docker compose down
```

`docker compose down` 不会删除数据。Compose 固定使用命名卷 `code-switch-r_codeswitch-data`，容器内 `HOME=/data`，进程以 UID/GID `10001:10001` 运行，重启策略为 `unless-stopped`。

## 配置 Codex

容器不会挂载、读取或修改宿主的 `~/.codex`。请在宿主的 `~/.codex/config.toml` 手动添加本地 provider：

```toml
model = "gpt-5.6-sol"
model_provider = "codex_plus"

[model_providers.codex_plus]
name = "Codex++"
base_url = "http://127.0.0.1:18100"
wire_api = "responses"
```

relay 本身不要求客户端凭据。上游 API Key 只在 Codex++ WebUI 中配置；普通 Web API 默认不返回明文，编辑供应商时必须点击眼睛按钮才会显式读取。默认健康探测模型为 `gpt-5.6-sol`，供应商可以单独覆盖。

## Volume 迁移

启动时会在 `/data` 内检查已有数据库，并在数据库和写入队列启动前执行一次性迁移。支持以下 volume 内路径：

```text
/data/.code-switch/app.db
/data/app.db
/data/code-switch/app.db
/data/.config/code-switch/app.db
```

迁移会保留 Codex 供应商配置、应用设置、Codex 请求日志与统计、抓包会话、黑名单和健康历史，移除旧平台数据，并记录幂等迁移标记。原库会先生成 SQLite 一致性备份：

```text
/data/.code-switch/backups/app.db.before-codex-plus-v1.sqlite
```

如果旧库无法读取，会保留原文件及不可读副本并中止容器启动；不会接触宿主目录。根目录旧布局中的 `codex.json` 和 `app.json` 会迁入 `/data/.code-switch/`。

## WebUI 功能

- Responses 兼容供应商、备用地址、认证方式、TLS 与请求清理
- Level 路由、同级轮询、并发限制、模型白名单与映射
- 供应商费用倍率和每日费用限额
- 可用性检查（独立间隔、失败拉黑、恢复解禁）、HTTP 延迟测速与 L1-L5/固定时长黑名单
- 请求日志、Token/费用统计、历史保留和模型价格同步
- 默认关闭的完整请求抓包

固定时长黑名单支持手动设置 `1–10080` 分钟，默认 `30` 分钟。完整抓包可能保存请求头、请求体、响应和明文凭据，仅在排障时短暂开启。

## 本地验证

```bash
docker compose config --quiet
docker compose build
```

镜像版本为 `v2.6.44-codexplus.2`，可通过 `http://127.0.0.1:8080/api/info` 查看。

## License

详见 [LICENSE](LICENSE)。
