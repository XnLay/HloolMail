**HloolMail 变更审查 — 2026-09-25**

**修复交付（2026-09-25）**

用户已授权修复，下列 2 项 P2 均已处理。

- 认证职责已重新划分：[AuthPanel.tsx](/home/x/project/github/HloolMail/web/src/features/auth/AuthPanel.tsx) 保留导航、公开设置和跨 Tab 草稿；[LoginForm.tsx](/home/x/project/github/HloolMail/web/src/features/auth/LoginForm.tsx) 与 [RegistrationForm.tsx](/home/x/project/github/HloolMail/web/src/features/auth/RegistrationForm.tsx) 分别管理各自的提交和临时状态。删除了返回 55 个成员的 `useAuthFlow`；验证码邮件轮询归位 `useVerificationDelivery`，仅返回进度与提示。字段控件统一标签、错误关联和输入处理。
- 注册信息变化通过唯一入口清空验证结果和验证码，并提升请求版本。迟到响应不能恢复旧验证；已消耗的人机验证凭据会刷新；已卸载表单的回调不能修改父面板草稿。表单切换通过组件生命周期清理临时状态，保留邮箱、密码、昵称、确认密码及挑战答案草稿。
- [release-binaries.yml](/home/x/project/github/HloolMail/.github/workflows/release-binaries.yml:130) 下载限定 `hloolmail-*`，发布限定 `hloolmail-*.tar.gz` 和 `hloolmail-*.zip`，并启用 `fail_on_unmatched_files`。覆盖率 artifact 继续用于 CI，不再进入 Release。

本次验证：前端 20 个测试文件、164 条测试通过（新增 12 条认证回归，整套约 15.04 秒）；ESLint、TypeScript、变更文件 Prettier、`git diff --check` 和生产构建通过。发布选择核验包含五个平台压缩包、覆盖率及日志，确认仅选择五个预期压缩包。

生产构建的 Chrome 检查覆盖 1440×1000 和 390×844：键盘切换、草稿保留、验证失效、错误 ARIA 关联及 Turnstile 切换生命周期均通过，无页面异常和横向溢出。截图检查发现并修正了验证码输入框在新容器中未铺满列宽的问题；最终输入框宽度与所在列一致。浏览器使用模拟 API 与 Turnstile，不代表真实 OAuth、Passkey 设备或 Cloudflare 服务联调；未执行 GitHub 发布。本次未修改 Go 源码，后端验证记录保留在下方初次审查中。

设计原则：KISS 使用组件生命周期清理临时状态；SOLID 让登录、注册和投递状态分别拥有职责；DRY 统一字段控件及验证失效规则；YAGNI 复用现有 React/TanStack Query，无新增状态管理依赖。原两个认证文件共 1,027 行，替代它们的六个职责模块共 894 行，最大文件 322 行。

原始文件快照、浏览器脚本和截图保存在 `/tmp/hloolmail-review-fixes-_ujeqp17/`。下一步可审阅当前 diff，由后续正常 CI 执行发布门禁。本次没有 commit、push 或部署。

**以下保留修复前的审查记录**

结论：需要修改。保留 2 项 P2：认证抽象的状态边界问题，以及发布产物混入覆盖率报告的问题。前者属于严格可维护性审查发现，后者属于本次工作流组合引入的行为回归。

范围为 `main@b4e2135` 到当前工作区的已跟踪及未跟踪变更。已读取上一轮审查和修复 Memory，使用 `code-review` 技能；本轮审查未修改业务代码、执行提交或触发发布。

1. **[P2] 认证抽取应封装状态转换，而不是暴露整套内部状态。**

   修复前位置：`web/src/features/auth/useAuthFlow.ts:504`（现已删除），调用方为当时的 `AuthPanel.tsx:194`。

   新增的 `useAuthFlow` 返回 55 个成员，包括原始 setter、查询和 mutation 对象、DOM ref、文案与派生标志；502 行的 `AuthPanel` 几乎完整消费了 525 行 hook 的内部模型。尤其是“注册信息变化后必须作废验证状态”的规则仍分散在昵称、邮箱、密码和确认密码四个输入处理器中：调用方要自己依次清错误、`setVerification(null)`、清空验证码。修改注册字段或验证流程时仍必须同时理解和修改两个文件，新增 hook 没有拥有它应维护的状态约束。

   首页与认证生命周期分离是有效改进，但认证内部的这条新边界仍只是把复杂度跨文件传递。建议按登录表单、注册验证流程及验证码组件划分职责，让字段更新与验证失效由同一模块负责，对外只暴露明确的值和操作；避免仅把这 55 个成员重新分组。重构应保留现有 tab 切换时的字段保留行为。

   验收：展示层不再直接调用验证状态 setter；字段变化和模式切换的状态转换有单一实现，现有登录、注册验证、Passkey、OAuth 和验证码行为保持一致。

2. **[P2] 接入质量工作流后，Release 会额外发布 `coverage.out`。**

   引入位置：[quality.yml:44](/home/x/project/github/HloolMail/.github/workflows/quality.yml:44)，组合入口：[release-binaries.yml:16](/home/x/project/github/HloolMail/.github/workflows/release-binaries.yml:16)，消费位置：[release-binaries.yml:130](/home/x/project/github/HloolMail/.github/workflows/release-binaries.yml:130)。

   可复用质量工作流会在调用方运行中上传名为 `go-coverage`、内容为 `coverage.out` 的 artifact。二进制发布新增调用后，原有下载步骤仍没有 `name` 或 `pattern`，并启用 `merge-multiple: true`，会把覆盖率文件与五个平台的压缩包一起放进 `dist`；随后发布步骤的 `files: dist/*` 会把覆盖率报告作为正式 Release 附件上传。

   建议在发布下载步骤使用 `pattern: hloolmail-*`，并把发布文件列表限定为预期的压缩包类型。质量报告可以继续作为 CI artifact 保存。

   验收：同一次运行存在 `go-coverage` 与平台二进制 artifacts 时，Release 输入列表只包含五个平台的压缩包，不包含 `coverage.out`。该发现已通过工作流输入输出的静态链路核验，未触发实际 GitHub 发布。

**结构核验与原则应用**

- HTTP 主处理器从 2,764 行降至 69 行；原 110 个函数中 106 个函数体保持不变，其余 4 个改为调用共享删除逻辑。邮箱删除和附属数据清理由 `mailstore` 统一，减少了需要同步维护的事务规则，符合 DRY 和职责分离。
- 首页从 1,378 行降至 505 行，营销动画与认证生命周期已分离。没有文件因本次变更从不足 1,000 行跨到超过 1,000 行；既有超大测试文件未被误记为新增体积回归。
- KISS、SOLID 的后续落点是缩小认证状态边界，让规则拥有明确归属；YAGNI 的落点是直接组合现有表单和查询能力，不为收拢状态再增加通用框架。
- 大量代码搬迁容易掩盖实际行为差异。本轮先比对搬迁函数，再追踪事务、归属迁移、worker 领取与会话清理调用链，避免把未变的旧问题归因于本次变更。

**本轮验证**

| 检查 | 结果 |
| --- | --- |
| `GOPROXY=off go test -timeout=60s ./...` | 全部通过；数据库包约 7.44 秒，HTTP 包约 4.27 秒 |
| `npm test` | 20 个文件、152 条测试通过，约 48.79 秒 |
| `go vet ./cmd/... ./internal/...` | 通过 |
| ESLint、TypeScript 类型检查 | 通过 |
| `gofmt -l internal cmd` | 无输出 |
| 本次前端变更的 Prettier 检查 | 通过 |
| `git diff --check` | 通过 |
| 认证 hook 返回契约 | TypeScript AST 确认返回 55 个成员 |
| 工作流产物链路 | 确认覆盖率 artifact 会被无筛选下载及发布规则收集 |

动态验证使用 SQLite 与 jsdom。本轮未运行真实 PostgreSQL、浏览器端到端测试、Docker 构建或 GitHub Actions，也未重跑生产构建和 race 检查。

下一步先限制发布产物范围，再收紧认证状态边界，并补充相应产物筛选与状态转换验证。测试通过不能替代这两项边界修正。
