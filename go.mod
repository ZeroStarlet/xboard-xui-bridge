// xboard-xui-bridge 模块定义。
//
// 依赖说明（构建时由 `go mod tidy` 解析具体次版本）：
//
//   - modernc.org/sqlite：纯 Go 的 SQLite 实现，避免 cgo 跨平台编译困难，
//     代价是体积较大（约 5MB），单进程内并发性能略弱于 mattn/go-sqlite3，
//     但本中间件单实例 QPS 远低于其上限，权衡后选择构建友好性。
//
// v0.2 起的依赖变化：
//
//   - 删除 gopkg.in/yaml.v3（v0.1 用作 config.yaml 解析）：配置真相源
//     已迁移到 SQLite settings / bridges 表，不再需要 YAML。
//   - 新增 golang.org/x/crypto（M4 阶段引入）：bcrypt 用于管理员密码哈希；
//     选 cost=12，登录校验 ~150ms，对暴力破解极不友好。
//   - 新增 golang.org/x/term（M7 阶段引入）：reset-password CLI 子命令读取
//     新密码时隐藏明文输入，跨平台支持。
//
// 任何新增依赖都必须在此说明动机，避免 go.sum 出现"凭空冒出"的依赖项。
module github.com/xboard-bridge/xboard-xui-bridge

go 1.25.0

require (
	golang.org/x/crypto v0.51.0
	golang.org/x/term v0.43.0
	modernc.org/sqlite v1.34.4
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.44.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)
