// Vite 配置文件。
//
// 输出策略：
//
//   - outDir = 'dist' 相对于 web/，最终路径 web/dist/
//   - 构建脚本（M8 阶段 Makefile 的 web target）会把 web/dist 复制到
//     internal/web/dist 让 Go 的 //go:embed 打入二进制。
//
// 开发期代理：
//
//   - 本地起 Go 后端（默认 127.0.0.1:8787）+ 起 Vite dev server（5173）；
//   - Vite 把 /api 前缀的请求转发到 8787，避免 CORS 与 cookie 跨域问题。
//   - 这样前端开发不需要 stub 后端，所有 fetch 直接打真实接口。
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // 单文件 chunk 上限设大一点：本面板代码量小，chunk 拆分意义不大。
    chunkSizeWarningLimit: 800,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8787',
        changeOrigin: false,
      },
    },
  },
})
