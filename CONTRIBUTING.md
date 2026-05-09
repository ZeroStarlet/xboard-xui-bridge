# 贡献指南

本项目采用极致严苛的代码风格与可维护性约束。任何变更都必须通过下方约定，否则合并请求会被驳回。

## 1. 风格铁律

- 新增或修改代码，必须在命名、缩进、注释格式、错误处理等所有维度与同文件既有代码保持一致。
- 同一文件存在历史不一致时，以"最近一次大规模重构形成的风格"为基准，修改前后通过注释声明取舍理由。
- 注释必须详尽到"未接触本项目的开发者仅凭注释即可理解并安全改写"。
- 注释必须与实现同步更新，不得保留过时的描述。
- 严禁占位实现、TODO、空函数体；功能必须完整可执行。

## 2. Go 项目约定

- 模块路径：`github.com/xboard-bridge/xboard-xui-bridge`
- 依赖管理：通过 `go mod tidy` 维护，禁止手工编辑 `go.sum`。
- 静态检查：所有 PR 必须 `make vet` 通过。
- 编译：`make build` 在 Linux/Windows/macOS 任一主流平台均能产出二进制。
- 包结构：业务包必须放在 `internal/` 下，禁止暴露给外部模块。

## 3. 协议与遥测语义不可破坏

- 流量遥测必须遵循"差量计算 → 先上报后推进基线"的契约。任何修改都必须在 PR 描述中解释为何不会破坏"重启不丢失、不重复计费"的不变量。
- 协议适配器必须保证 `BuildClient` 输出与 3x-ui inbound.protocol 字段强匹配；新增协议时优先扩展 `protocol` 包，不在 `sync` 包内塞分支。
- 鉴权 / 会话相关变更必须保持"改密强制下线"原子性——使用 `store.UpdateAdminPasswordAndPurgeSessions` 单事务，不要拆成两步。

## 4. 配置变更（v0.2 起的"零 yaml" 流程）

任何新增 / 重命名配置项都必须四处同步——缺一会导致用户写入的字段被静默忽略：

  1. `internal/config/config.go` 顶部 `Setting*` 常量声明 key（命名约定 `<group>.<field>`）；
  2. `internal/config/config.go: LoadFromStore` 把该 key 映射到 `*config.Root` 字段；
  3. `internal/config/config.go: Validate` 完成默认值补齐与格式校验（"全空允许 / 半填严格"原则）；
  4. `internal/web/settings_handler.go` 的 `settingsPatchRequest` + `buildSettingsKV` 表单层。

> **数据库不再有 yaml 解析的 `KnownFields(true)` 护栏**——SQLite 写入时不会拒绝拼错的 key，所以 `Setting*` 常量是唯一真相源；任何手写字符串都属于风格违规，PR 审查中必须用常量替换。

历史背景：v0.1 通过 `gopkg.in/yaml.v3` 的 `KnownFields(true)` 阻止配置拼写错误；v0.2 改用 SQLite KV 后，这层护栏由"Web 表单写入路径只接受 `Setting*` 常量列出的 key"承担——具体实现详见 `internal/web/settings_handler.go` 的白名单逻辑。

## 5. 前端变更（v0.2 起的 SPA 流程）

前端代码在 `web/` 目录，使用 Vue 3 + Vite + Tailwind + TypeScript。任何前端 PR 必须：

  1. **本地构建通过**：`cd web && npm run build` 无 TypeScript / 编译错误；
  2. **后端同步**：若改动涉及新 API，必须先在 `internal/web/*_handler.go` 实现并通过 Codex 审核，再在 `web/src/api/client.ts` 加 typed 方法；
  3. **类型严格**：尽量避免 `any`；如必要请加注释说明动机；
  4. **可访问性**：模态层加 `role="dialog" aria-modal="true" aria-label="…"`；图标按钮加 `aria-label`；不依赖 emoji 作结构图标；
  5. **Embed 同步**：构建产物 `web/dist` 不直接入仓；通过 `make web` 一次性同步到 `internal/web/dist/`（go:embed 真相源）后再 commit。

## 6. 提交流程

1. 先通过 issue / 讨论确认需求与方案。
2. 本地完成实现 + `make vet test` 全绿（前端改动还要 `npm run build` 通过）。
3. 提交 PR，描述包含：动机、改动概要、潜在风险、测试结果。
4. 通过 Codex / 同行审查的全部问题闭环之后方可合并；遗留问题不予合并。

## 7. 模块边界

- `internal/config` 不依赖 web / supervisor / auth；它是纯计算层。
- `internal/web` 通过 `auth.Service` + `supervisor.Supervisor` 接口操作引擎，不直接 reach 进 store 之外的细节。
- `internal/supervisor` 的 `Reload` 必须保持"先构造新引擎再切旧引擎"语义——任何改动都必须保证 buildEngine 失败时旧引擎仍在跑。
- `internal/auth` 不持有 HTTP / cookie 概念；这些由 `internal/web/middleware.go` 翻译。

边界违反在 code review 中会被作为 P1 拒绝。
