// 入口：装配 Pinia + Router + i18n + 全局 style，挂载根组件。
//
// 故意保持极简——所有组件都在按需懒加载（router/index.ts 做的），
// 避免主入口臃肿。
//
// v0.6 新增装配：
//   - i18n（vue-i18n）插件——让组件 t()/d()/n() 函数可用
//   - 主题 store 初始化——读 localStorage + 应用 .dark class（在 mount 前
//     完成，避免 flash of wrong theme）
//   - 语言 store 初始化——读 localStorage 或 navigator.language + 同步
//     i18n.global.locale.value + 写 <html lang>
//
// 装配顺序（推荐顺序，部分步骤之间无强依赖但保持此顺序符合维护直觉）：
//
//   1. createApp(App) — 创建应用实例
//   2. createPinia + app.use — 装 store 框架，让 useXStore() 可用
//   3. app.use(router) — 装路由
//   4. app.use(i18n) — 装 i18n plugin，让组件里的 useI18n() / t() 可用
//   5. useThemeStore() — 触发 store 初始化 + watch immediate 应用 .dark class
//   6. useLocaleStore() — 触发 store 初始化 + watch immediate 写
//      i18n.global.locale.value 与 <html lang>
//   7. useAuthStore() + 401 handler — 路由跳登录页的全局 hook
//   8. app.mount('#app') — 渲染首屏
//
// 强依赖（不可调换）：
//   - 1→2：Pinia 必须先于任何 useXStore() 调用 install。
//   - 5/6 必须在 mount 之前：immediate watch 在 store 初始化时立刻同步 .dark
//     class / html lang / i18n locale，让首屏渲染就拿到正确状态，避免用户
//     看到"先亮一下再切深"的视觉跳变（FOIT/FOUT 风格的小坑）。
//
// 弱依赖（技术上可调换，保持当前顺序仅为"维护直觉"）：
//   - 5 与 6 之间互不依赖，先 theme 后 locale 是因为视觉切换比文案切换
//     更"敏感"，先把 .dark class 应到 DOM 上。
//   - 4 与 6 之间：vue-i18n 的 i18n.global 在 createI18n() 调用后立即可用，
//     即便 app.use(i18n) 还没跑也能写 i18n.global.locale.value；保持
//     "先 use 再 use 相关 store"是为了避免维护者误以为 store 必须自带
//     plugin install——当前顺序更接近"标准 Vue 应用启动流程"。
//
// 全局 401 handler 在此装配：让 api/client.ts 在收到任意 401（除自身
// fetchMe / login 外）时统一清空 auth store + 跳转登录页，避免每个
// view 各自处理 401 留下"已登录但实际未登录"的卡死状态。
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './style.css'
import { i18n } from './i18n'
import { setUnauthorizedHandler } from './api/client'
import { useAuthStore } from './stores/auth'
import { useThemeStore } from './stores/theme'
import { useLocaleStore } from './stores/locale'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.use(i18n)

// 触发主题 / 语言 store 初始化——构造函数内的 watch immediate 会同步把
// .dark class、<html lang>、i18n.global.locale 应用到 DOM 与 i18n 实例，
// 让首屏渲染就拿到正确的外观与语言。返回值不需保留（store 通过模块级
// 单例 + Pinia getter 重新拿到）。
useThemeStore()
useLocaleStore()

// 装配 401 全局 handler。必须在 Pinia 安装之后调用 useAuthStore。
const authStore = useAuthStore()
setUnauthorizedHandler(() => {
  authStore.user = null
  // 当前路径作为 redirect 参数，登录后跳回。
  const cur = router.currentRoute.value.fullPath
  router.push({ path: '/login', query: cur && cur !== '/login' ? { redirect: cur } : {} })
})

app.mount('#app')
