# Services Tests

服务层测试与实现放在同一个 Go package 中，重点覆盖 Codex 供应商、Responses 转发、故障切换、日志、价格计算、历史清理和数据库写入队列。

## Run

```bash
go test ./services -count=1
go test ./resources/model-pricing -count=1
go test ./... -count=1
go vet ./...
```

涉及并发快照或写队列时，可额外运行：

```bash
go test -race ./services ./resources/model-pricing
```

## Fixtures

测试夹具放在 `services/testdata/` 或对应 package 的测试目录。供应商示例使用平台 `codex`、模型 `gpt-5.6`、端点 `/responses` 和 Bearer 认证。

新增平台参数测试时，应同时断言：

- `codex` 被接受；
- 空字符串和其他值返回稳定的不支持平台错误；
- 旧应用数据不会重新注册路由或写入外部配置。

修改 WebUI 可调用的 service 方法后，同步更新 `main.go` 中的 HTTP RPC 白名单和 `frontend/src/services/` 封装，再运行全量测试。
