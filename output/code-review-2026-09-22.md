**HloolMail 严格代码审查 — 2026-09-22**

审查结论：需要修改。保留 8 项高价值发现：3 项 P1、5 项 P2，其中 5 项功能缺陷已通过本地边界用例复现。

基线为 `main@b4e2135`（v0.9.2）。本次按整项目审查，包含既有设计问题，不将其全部归因于最近一次发布。审查前工作区干净。本次交付审查报告和 Memory，未修改业务源代码。

重点覆盖 HTTP 与会话边界、归属与迁移、SMTP 入库、异步投递、前端查询和认证结构，以及 CI。结构盘点涉及 334 个源码、测试和样式文件，共 89,662 行；生成类型、语言资源和样式的体积没有被直接认定为业务结构缺陷。

1. **[P1] 启动回填会把历史邮件归给后来创建同名邮箱的账号。**

   位置：[db.go:329](/home/x/project/github/HloolMail/internal/db/db.go:329)，调用入口：[db.go:205](/home/x/project/github/HloolMail/internal/db/db.go:205)。

   当数据库存在 `owner_id IS NULL` 的历史邮件，之后又创建同名邮箱时，回填仅通过 `mailboxes.email = messages.recipient` 写入当前 owner、mailbox 和 domain。代码没有证明收件时的归属，也没有检查邮箱创建时间；生产启动每次都会执行这段回填。私有域名的回填也依赖当前域名归属。

   复现直接扩展现有 `TestRecreatedMailboxDoesNotSeeLegacyUnownedMessages`：先确认新邮箱看不到历史邮件，再执行生产启动使用的 `AutoMigrate`。随后历史邮件详情返回 **200 和正文，原预期为 404**。运行时已经存在的 owner 隔离被启动迁移绕过。

   建议把历史归属迁移变成有版本的一次性操作，要求可证明的历史关联；无法证明的记录保持隔离。当前编号 SQL 迁移只有空基线，实际升级和回填仍依赖每次启动的 `AutoMigrate`，应统一升级入口。验收必须包含“创建同名邮箱后重启仍无法取得旧邮件”。

2. **[P1] SMTP 对多收件人逐个提交，后续失败会造成重试重复投递。**

   位置：[backend.go:161](/home/x/project/github/HloolMail/internal/smtp/backend.go:161)。

   `Data` 在收件人循环内部开启事务。前一个收件人的邮件、附件、统计和 Webhook 已提交后，后一个收件人写库失败会对整次 DATA 返回 451。发送方随后重试整封邮件，之前成功的收件人再次收到内容。

   在第二个收件人写库时注入错误，第一位收件人仍保留 **1 条已提交邮件**；重试后变成 **2 条**。Webhook 去重键包含新生成的 message ID，也不能消除这种重复。

   建议将一次 DATA 的全部收件人、附件、统计及 Webhook 入队纳入同一事务，提交完成后再发布内存事件。验收要求任一收件人失败时，整次 DATA 不留下部分邮件或待投递事件。

3. **[P1] Gin 的默认代理信任使客户端能通过转发头绕过 IP 限流。**

   位置：[router.go:71](/home/x/project/github/HloolMail/internal/http/router.go:71)，消费位置：[middleware.go:475](/home/x/project/github/HloolMail/internal/http/middleware.go:475)。

   路由初始化没有配置可信代理，而当前 Gin 版本默认信任所有 IPv4/IPv6 代理。`perIPRateLimit` 直接使用 `c.ClientIP()`，因此直连 HTTP 端口，或上游保留客户端转发头时，请求者可以改变 `X-Forwarded-For` 来改变限流身份。登录、注册等入口共用此函数。

   复现使用相同 `RemoteAddr` 和一次性限流额度。仅把转发头从一个地址改成另一个地址，第二次请求仍返回 **204，预期为 429**，两次被接受的 client IP 都来自伪造头。

   建议在入口显式配置可信代理 CIDR；直接暴露服务时默认不信任转发头。验收需覆盖直连伪造头、可信代理转发以及多级代理链，保证登录限流和审计使用同一可信来源。

4. **[P2] Webhook 的旧 worker 在锁被接管后仍能发送并更新任务。**

   位置：[worker.go:262](/home/x/project/github/HloolMail/internal/webhook/worker.go:262)，完成更新：[worker.go:273](/home/x/project/github/HloolMail/internal/webhook/worker.go:273)。

   领取逻辑写入 `locked_by`，但 `deliveryStillClaimed` 只检查状态和 payload；尝试次数与成功、失败更新也未限定锁持有者。另一 worker 接管过期锁后，恢复执行的旧 worker 仍能通过检查。

   将接管时间推进 11 分钟，并在当前 worker 的模拟 HTTP 请求期间恢复旧 worker，同一投递实际发出 **2 次 HTTP 请求，而数据库 attempt_count 为 1**。这是任务所有权检查缺失，普通数据竞争检测不一定能发现。

   建议让发送前检查、尝试计数和完成更新都使用包含 `locked_by` 的领取条件。邮件投递 worker 已有类似约束，两者应采用一致的最小领取协议。验收要求锁被接管后旧 worker 既不能发送，也不能改变新持有者的状态。

5. **[P2] 退出登录后，延迟返回的身份请求会恢复旧用户缓存。**

   位置：[queryClient.ts:123](/home/x/project/github/HloolMail/web/src/lib/queryClient.ts:123)。

   `clearUserSession` 保留 `me` 查询并直接写入匿名值，但没有取消正在进行的身份请求。清空 GET 去重表不会阻止已有查询的成功回调。

   使用延迟 Promise 复现：退出时 `user` 已变为 `null`，旧身份请求随后完成，查询缓存又被写回旧账号。该问题会造成界面错误恢复登录态；它不代表后端会重新授予已撤销的会话权限。

   建议在写入匿名状态前取消身份查询，并把查询的 `AbortSignal` 传给请求层，使用 TanStack Query 的取消机制统一处理在途请求。验收包含退出登录、身份切换和旧请求晚到三种时序。

6. **[P2] 巨型 HTTP 处理器仍承载重复的邮箱生命周期事务。**

   位置：[handlers.go:2549](/home/x/project/github/HloolMail/internal/http/handlers.go:2549)，重复实现：[yyds_compat.go:293](/home/x/project/github/HloolMail/internal/http/yyds_compat.go:293)。

   `handlers.go` 已达 **2,764 行、110 个函数**，混合统计、域名、邮箱、API Key、配额和查询策略。普通 API 与兼容 API 又分别实现完整的邮箱删除事务，包括相同的归属 SQL、分享删除和邮件依赖清理。新增生命周期规则时必须同步修改多个入口。

   建议先提取唯一的邮箱生命周期操作，让两个协议适配入口共享同一事务；这可直接删除重复实现。随后按统计、域名、API Key、邮箱拆分处理器职责，将复杂配额和归属规则归位。验收应确认两个协议入口共用同一实现，现有隔离、删除、配额用例继续通过。

7. **[P2] 营销首页与认证流程被 authMode 绑定在一个巨型组件中。**

   位置：[LandingPage.tsx:50](/home/x/project/github/HloolMail/web/src/pages/LandingPage.tsx:50)，包装入口：[LoginPage.tsx:13](/home/x/project/github/HloolMail/web/src/pages/LoginPage.tsx:13)。

   `LandingPage.tsx` 有 **1,378 行**，同时持有营销滚动效果、导航观察、登录、注册、验证码、验证邮件和 Passkey 逻辑。`LoginPage` 仅通过 `authMode="auth"` 调用它；多个 effect 和渲染分支再判断是否为认证页面。两个独立场景因模式开关而共享生命周期和状态。

   建议把认证流程及表单归位独立 feature，让首页和登录页组合各自需要的组件，移除跨场景的 `authMode` 分支。共享的公开登录设置查询可以继续复用。验收要求首页不实例化认证表单状态，登录页不持有营销滚动逻辑，认证流程能单独测试。

8. **[P2] CI 没有运行现有前端测试，回归用例无法形成发布门禁。**

   位置：[ci.yml:66](/home/x/project/github/HloolMail/.github/workflows/ci.yml:66)。

   当前工作流运行 `check:api`、ESLint、TypeScript 和构建，但没有执行 `npm test`。仓库已有 18 个前端测试文件、149 条测试；新加入的 store 和布局断言也不会被这些构建检查执行。

   建议在依赖安装后加入 `npm test`，并让发布依赖该检查结果。验收可以通过故意令一条前端断言失败，确认 CI 和发布流程确实阻止继续。

**验证记录**

| 检查 | 结果 |
| --- | --- |
| 原有 Go 测试 | 分批累计 358 条通过，包含子测试；数据库 22 个顶层用例全部覆盖 |
| 原有前端测试 | 18 个文件、149 条通过 |
| 审查新增边界断言 | 5 条均复现缺陷：历史归属、SMTP 部分提交、代理信任、Webhook 接管、退出登录竞态 |
| Go vet | 通过，范围为 cmd 与 internal |
| Go 格式检查 | `gofmt -l internal cmd` 无输出 |
| TypeScript | `tsc --noEmit -p tsconfig.json` 通过 |
| ESLint | 196 个文件，0 错误、0 警告 |
| 前端生产构建 | 通过，Vite 构建阶段约 12.04 秒 |
| Prettier | 未通过，188 个文件不符合当前配置 |

Go 数据库包在整包 45 秒时限下超时。栈和慢查询记录显示耗时集中于 SQLite 建表的 fsync；更换临时目录未消除该环境开销。已通过的前 12 个数据库用例保留结果，剩余 10 个分为四批补验，各批约 31.9–36.3 秒，均小于 60 秒。上述 Go 通过数汇总自这些运行，并非一次整包运行在 60 秒内完成。

动态后端验证使用本地 SQLite；Webhook 使用模拟 HTTP transport，前端竞态使用 jsdom 和延迟 Promise。未接入真实 PostgreSQL 验证。临时复现源码、配置与日志在 `/tmp/hloolmail-review.y75Spd/`，其中 `validation-summary.json` 保存检查汇总。复现测试通过 Go overlay 和独立 Vitest 配置运行，未写入业务测试目录。

可从项目根目录重跑四项 Go 缺陷复现：

```bash
go test -overlay=/tmp/hloolmail-review.y75Spd/go-overlay.json -run '^TestReview' -timeout=45s ./internal/http ./internal/smtp ./internal/webhook
```

在 `web` 目录重跑前端缺陷复现：

```bash
./node_modules/.bin/vitest run --config /tmp/hloolmail-review.y75Spd/vitest.review.config.mjs
```

这两条命令在当前实现上预期失败，失败断言对应上文的已确认缺陷。

**修复状态（2026-09-24）**

本报告中的 8 项发现均已在当前工作区修复，并加入回归测试。历史归属改为基于原始关联的一次性数据迁移；SMTP DATA 改为整批原子事务；代理信任改为显式配置；Webhook 与邮件投递统一校验 worker 和领取时间；退出登录会取消身份查询；邮箱生命周期已集中到 `internal/mailstore`；首页和认证流程已拆分；统一质量工作流已接入 CI、二进制发布和 Docker 发布依赖。

最新验证：`go test ./...`、关键包 race、`go vet ./cmd/... ./internal/...`、`gofmt`、前端 152 条测试、ESLint、TypeScript、生产构建、API 快照检查和 Docker Compose 配置均通过。未执行真实 PostgreSQL 或浏览器端到端验证。

**实施顺序与原则**

先把五个复现纳入永久回归测试，并接入前端 CI 门禁；随后修复三项 P1，再修复 worker 和前端会话竞态，最后统一邮箱生命周期、拆分认证与首页。结构调整时持续保留 owner、配额和兼容协议行为的现有测试。

KISS 与 YAGNI 的落点是复用 GORM 事务、TanStack Query 取消和现有测试框架。SOLID 的落点是让迁移、历史归属、生命周期操作和协议呈现各自承担清晰职责。DRY 的落点是删除重复的邮箱删除事务，并统一 worker 的领取约束。这些调整应减少需要同步维护的规则和模式开关。

审查方法与约束：首先读取项目约束和安全模型；初始没有项目 Memory。TypeScript 使用 Serena 符号概览、符号读取和引用追踪，Go 使用定向源码与本地工具核验。安全模型明确允许受会话鉴权和审计保护的 API Key 查看，本次没有把这一既定产品能力误报为缺陷。
