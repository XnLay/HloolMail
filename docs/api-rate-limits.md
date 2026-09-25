# API 请求限流配置与运维

管理员可在 **管理后台 → API 接入 → API 请求限流** 调整入口保护与业务速率。保存后无需重启。七项规则均可单独停用，停用时仍保留合法参数，方便恢复。

## 默认配置与计数范围

| 规则 | 每秒请求数 | 突发额度 | 计数范围 |
| --- | ---: | ---: | --- |
| 单来源 IP | 5 | 20 | IP＋HTTP 方法＋路由模板，包括无效 Key 请求 |
| 单实例合计 | 50 | 200 | 当前实例内，同一 HTTP 方法＋路由模板的全部来源 |
| 邮件与邮箱 | 2 | 20 | API Key＋HTTP 方法＋路由模板 |
| 生成邮箱 | 1 | 10 | API Key＋`POST /api/generate-email` |
| 可用域名 | 0.5 | 5 | API Key＋`GET /api/domains/available` |
| 工作区统计 | 2 | 10 | API Key＋`GET /api/stats` |
| YYDS 兼容接口 | 2 | 20 | API Key＋兼容接口的 HTTP 方法＋路由模板 |

默认值由 `internal/apiratelimit.DefaultConfig` 定义，面板从服务端读取默认值与校验边界。首次升级插入默认设置；重启和再次升级保留已保存的配置。YYDS 设置不会自动开启兼容接口。

速率必须为有限十进制数，范围为 `0.001～100000`；突发额度必须是 `1～100000` 的整数。关闭规则使用开关，不能以 `0` 表示不限。突发额度是可积累并立即使用的请求次数，不是并发连接数；上下限是输入边界，不代表服务吞吐承诺。

同一分组中的接口使用相同参数，但分别计数。例如邮箱列表与邮件详情各有自己的桶；同一 Key 查询两个不同邮箱、页码或邮件 ID，只要方法和路由模板相同，仍共用额度。多个 Key 来自同一 IP 时共享 IP 入口额度，同一 Key 来自多个 IP 时仍共享其业务额度。

单 IP、单 Key、单接口的理论持续上限取所有启用规则的最小速率。把业务速率提高到 100/s，而 IP 入口仍为 5/s，实际仍会受 5/s 入口约束。面板会提示这一瓶颈；其他调用者、密钥校验成本和数据库负载也会影响实际吞吐。

## 保存、撤销与生效

“填入默认值”只修改草稿，点击“保存限流设置”后才会生效。“撤销更改”回到最新已知的已保存配置。后台刷新保留未保存内容；把全部输入改回草稿基准值时，会自动显示最新快照。草稿及其操作者、更新时间绑定同一基准版本；离开页面时会提示未保存变更。

设置、版本与审计在同一事务提交。审计动作是 `api_rate_limit_settings.update`，记录操作者、前后版本和完整配置快照。无实际变化的保存不会产生新版本或新审计。失败时配置与审计一起回滚。

保存后本实例立即发布新配置，其他实例每 5 秒同步一次；正常情况下在下一次同步完成后生效。同步失败保留最后有效配置并记录日志与指标。启动时无法读取有效配置会明确失败。配置版本只前进，迟到读取不能覆盖新配置。

已有桶在下一次访问时原地采用新参数，更新不会额外赠送完整突发额度。无请求期间积累的令牌按桶此前的参数结算，不回溯重算期间发生过的所有配置变化。重新启用的已有桶仍保留已消耗额度。空闲回收必须同时满足 10 分钟 TTL 和令牌已自然补满；低速率、大突发配置会保留更久，回收后重建不会增加本应获得的额度。

API 桶与登录、管理员、会话保护使用独立存储，各自默认最多 100,000 个桶。容量满时只检查最早可回收的候选桶，且只有自然补满的桶可被替换；没有可安全回收的候选时，新来源返回带 `Retry-After` 的 `429`，已有桶继续正常计数。保存配置不会清空桶或绕过容量保护，管理员仍可访问设置入口。

**多实例只共享配置，令牌计数按实例独立。** “单实例合计”是每个方法/路由的合计，不是全站全部接口合计，也不是集群总额。严格集群限流需要额外的共享计数后端。

## 管理接口

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| `GET` | `/api/admin/rate-limit-settings` | 读取配置、`revision`、`defaults`、`constraints`、更新时间/操作者和本实例 `applied_revision` |
| `PUT` | `/api/admin/rate-limit-settings` | 携带当前 `revision` 和完整 `config`，原子保存所有规则 |

`config.pre_auth` 包含 `per_ip`、`per_instance`；`config.business` 包含 `mail`、`generate_email`、`available_domains`、`stats`、`yyds`。每条规则必须包含 `enabled`、`requests_per_second`、`burst`。缺失、未知字段、类型错误和越界数值返回 `400`。

接口只接受管理员 Web 会话，普通用户、API Key、旧管理 Token 返回 `403`；浏览器写入必须同源。旧 `revision` 返回 `409`，面板保留草稿，要求显式加载最新设置再编辑。存储或审计失败返回 `500`，运行中的配置不变。

登录注册、管理员入口、公开分享、SSE 和普通 Web 会话限流使用独立固定规则。即使 API 阈值误设得很低，管理员仍可进入本面板，填入默认值并保存恢复。停用所有 API 速率规则不会绕过 Key、账号、owner 权限或每日/累计配额。

## 客户端重试与代理部署

API Key 调用顺序为：实例额度预检（不扣令牌、不分配桶）→ IP 入口 → 单实例入口扣额 → Key 与所属账号校验 → 业务限流 → 扣调用配额并记录用量 → 业务处理。实例已饱和时直接拒绝，不再创建来源桶；IP 拒绝不会消耗实例额度，预检后仍会在实际扣额时处理并发竞争。

任一速率层拒绝都返回 `429` 和现有错误信封中的 `rate limit exceeded`，另带 `Retry-After`：等待秒数向上取整且至少为 1。被限流拒绝的请求不扣每日/累计额度，也不写正常用量日志。限流器立即放行或拒绝，不排队或预占未来令牌。

调用配额耗尽也会返回 `429`，但不带上述速率重试提示；客户端需等待配额重置或调整配额。遵守 `Retry-After`，使用带抖动且有次数上限的退避；不要立即并发重试。持续收件通知优先使用 Webhook，减少轮询。

反向代理部署时，在 `TRUSTED_PROXIES` 中仅填写实际代理 IP/CIDR；直连时保持为空。未配置可信代理时，所有经同一代理的请求可能共享该代理 IP 的额度。参见 [README 的生产安全说明](../README.md#生产安全)。

## 监控与验证

启用现有 `/metrics` 后，可观察以下指标：

| 指标 | 含义 |
| --- | --- |
| `api_rate_limit_rejections_total{layer,policy,route}` | 各层、固定规则与路由模板的速率拒绝数 |
| `api_rate_limit_applied_revision` | 本实例应用版本 |
| `api_rate_limit_sync_failures_total` | 配置同步失败数 |
| `api_rate_limit_last_sync_timestamp_seconds` | 最近成功同步时间 |

指标标签不包含 Key、IP、邮箱或原始 URL。业务请求只读取内存快照，不查询限流设置表。持续同步失败时，检查数据库可达性和服务日志；已有请求继续使用最后有效配置。

普通单元测试不依赖 PostgreSQL。用独立测试库执行集成测试；每个用例创建并清理随机 schema，测试账号需有创建 schema 权限：

```bash
HLOOLMAIL_TEST_POSTGRES_DSN='postgres://testuser:testpass@127.0.0.1:5432/testdb?sslmode=disable' \
  go test -count=1 -timeout 60s ./internal/apiratelimit -run '^TestPostgres'
```

独立压测通过真实 HTTP 和 PostgreSQL 测试默认规则与提高阈值后的表现。测试密钥按 `key_value` 索引命中认证，请求不执行 bcrypt 比较；`stored_bcrypt_cost=12` 仅表示创建密钥时存储的哈希参数，`authentication_path=key_value_index` 明确记录认证路径。结果还包含成功/拒绝 RPS、成功请求 P95/P99、异常数、连接池等待和限流配置查询数。仅存哈希的历史密钥需作为独立场景评估：

```bash
HLOOLMAIL_TEST_POSTGRES_DSN='postgres://testuser:testpass@127.0.0.1:5432/testdb?sslmode=disable' \
HLOOLMAIL_RUN_RATE_LIMIT_LOAD=1 \
  go test -v -count=1 -timeout 60s ./internal/http -run '^TestPostgresAPIRateLimitLoad$'

go test -run '^$' -bench '^BenchmarkLimiter' -benchmem ./internal/ratelimit
```

压测结果只适用于对应机器、数据量和并发模型。提高阈值后还应根据目标部署的 CPU、数据库连接池和真实业务负载确定可承载规模。
