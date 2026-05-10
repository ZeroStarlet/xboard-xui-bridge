package web

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"strings"
)

// frontendFS 嵌入 M6 阶段构建产物 web/dist/。
//
// 路径相对于本文件所在的 internal/web/ 目录——所以 //go:embed 的目标
// 是项目根 ../../web/dist。Go 1.18+ 不允许 embed 路径包含 ".."，所以
// 实际打入二进制的是仓库内由 build 脚本拷贝到此目录的副本：
//
//   M8 阶段的 Makefile 会增加 `make web` 目标，做：
//     1) cd web && npm install && npm run build
//     2) rm -rf internal/web/dist && cp -R web/dist internal/web/dist
//   随后 `make build` 才能把最新前端打入二进制。
//
// 当前 M5 阶段尚无前端代码，本目录里仅放 placeholder 的 index.html，
// 让 //go:embed 能找到至少一个文件（embed 空目录会编译失败）。
//
//go:embed dist
var frontendFS embed.FS

// spaHandler 返回 SPA 静态资源 + 前端 router fallback。
//
// 行为：
//
//   - GET /assets/foo.js 等具体文件 → 在 dist/ 内找到则 serve，找不到 404
//     （让浏览器 dev tool 一眼看出"是真的没这个资源"，而非被 fallback 吞掉）；
//   - GET / 或任何"看起来像前端 router 路径"（不带文件后缀） → 返回 dist/index.html，
//     让前端 Vue Router 处理。
//
// 区分"文件 vs 路由"的判定：路径中最后一段是否含有 "."。这与 Vite / Vue
// 默认构建产物布局一致（资源都在 /assets/*.js / *.css 等带后缀路径）。
//
// 对于 M5 阶段 dist 仅含 placeholder index.html 的场景，所有 GET / 请求
// 都会返回那个 placeholder——它会显示"前端尚未构建，请运行 make web"
// 提示，让运维不至于以为 Web 面板挂了。
//
// 启动期缓存（v0.5.3）：index.html 在 spaHandler 构造时一次性 ReadFile 到
// indexBytes 闭包变量，所有 SPA fallback 路由复用同一份内存切片。embed.FS
// 的 ReadFile 是"从只读 segment 复制到新切片"操作（< KB 单次复制 + alloc），
// 单次成本极低；但在百万级路由命中下累积的 alloc 仍会触发更多 GC——一次
// 性 cache 让 serve 路径走零 alloc，更友好于长期运行的低带宽中间件。
func (s *Server) spaHandler() http.HandlerFunc {
	// 构造 dist 子文件系统：让 http.FileServer 看到的根就是 dist/，
	// 不必每次 r.URL.Path 都加 "dist/" 前缀。
	dist, err := fs.Sub(frontendFS, "dist")
	if err != nil {
		// init-time 错误（dist 目录被打包但拿不到）几乎不可能，仍兜底。
		s.log.Error("初始化 dist 子文件系统失败", "err", err)
		return func(w http.ResponseWriter, r *http.Request) {
			s.writeError(w, http.StatusInternalServerError, errCodeInternal, "前端资源未就绪")
		}
	}

	fileServer := http.FileServer(http.FS(dist))

	// 启动期缓存 index.html 字节内容，避免每次 SPA fallback 都触发 fs.ReadFile
	// 的 alloc。失败路径单独返回明确的错误 handler，让运维一眼看到"前端
	// 尚未构建"而非模糊 500——这与原 serveSPAIndex 内的 fs.ErrNotExist
	// 分支语义一致，只是把判定从"每次请求"前移到"启动一次"。
	indexBytes, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.log.Error("dist/index.html 不存在；请运行 make web 构建前端", "err", err)
			return func(w http.ResponseWriter, r *http.Request) {
				s.writeError(w, http.StatusInternalServerError, errCodeInternal,
					"dist/index.html 不存在；请运行 make web 构建前端")
			}
		}
		s.log.Error("读取 dist/index.html 失败", "err", err)
		return func(w http.ResponseWriter, r *http.Request) {
			s.writeError(w, http.StatusInternalServerError, errCodeInternal, "读取前端资源失败")
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// 仅处理 GET / HEAD；其它方法返回 405（避免 SPA 路由把 PUT 等
		// 误请求当作"找不到文件"返回 200 index.html，迷惑客户端）。
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.writeError(w, http.StatusMethodNotAllowed, errCodeMethodNotAllowed, "仅支持 GET / HEAD")
			return
		}

		// /api/* 不应到这里——server.go 的 mux 已按"具体优先"装好；
		// 但万一前端 router 配置错误把 /api/foo 当 SPA 路由，这里要拒
		// 绝以避免 fallback 到 index.html 让前端代码被当 JSON 解析。
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.writeError(w, http.StatusNotFound, errCodeNotFound, "API 路径不存在")
			return
		}

		// 判断是否"前端 router 路径"：去掉首 / 后再看最后一段是否含 "."。
		// 不含 "." 的视为 router 路径 → 直接返回 index.html。
		path := strings.TrimPrefix(r.URL.Path, "/")
		lastSlash := strings.LastIndex(path, "/")
		lastSeg := path
		if lastSlash >= 0 {
			lastSeg = path[lastSlash+1:]
		}
		if lastSeg == "" || !strings.Contains(lastSeg, ".") {
			s.serveSPAIndex(w, r, indexBytes)
			return
		}

		// 实际文件：交给 http.FileServer。它会自动处理 If-Modified-Since
		// 等缓存协议；不存在的文件返回 404。
		fileServer.ServeHTTP(w, r)
	}
}

// serveSPAIndex 把启动期已 cache 的 index.html 字节写入响应。
//
// 不走 http.FileServer：FileServer 对 / 路径会按目录列出文件（除非有
// index.html）；对自定义 router 路径（如 /bridges）则会返回 404。我们
// 想要的语义是"任何 router 路径都返回 index.html"——所以手工写字节流。
//
// indexBytes 由 spaHandler 启动期一次性 ReadFile（详见其注释），生命周期
// 与 Server 一致；本函数仅做 io.Copy 等价的"写出 + 设头"操作，无 alloc。
//
// Cache-Control: no-store：M5 阶段前端会频繁迭代；让浏览器永远拉最新。
// M8 上线稳定后可改为 stale-while-revalidate。
func (s *Server) serveSPAIndex(w http.ResponseWriter, _ *http.Request, indexBytes []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(indexBytes); err != nil {
		s.log.Error("写出 dist/index.html 失败", "err", err)
	}
}
