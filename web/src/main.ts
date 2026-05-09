// 入口：装配 Pinia + Router + 全局 style，挂载根组件。
//
// 故意保持极简——所有组件都在按需懒加载（router/index.ts 做的），
// 避免主入口臃肿。
//
// 全局 401 handler 在此装配：让 api/client.ts 在收到任意 401（除自身
// fetchMe / login 外）时统一清空 auth store + 跳转登录页，避免每个
// view 各自处理 401 留下"已登录但实际未登录"的卡死状态。
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './style.css'
import { setUnauthorizedHandler } from './api/client'
import { useAuthStore } from './stores/auth'

const app = createApp(App)
app.use(createPinia())
app.use(router)

// 装配 401 全局 handler。必须在 Pinia 安装之后调用 useAuthStore。
const authStore = useAuthStore()
setUnauthorizedHandler(() => {
  authStore.user = null
  // 当前路径作为 redirect 参数，登录后跳回。
  const cur = router.currentRoute.value.fullPath
  router.push({ path: '/login', query: cur && cur !== '/login' ? { redirect: cur } : {} })
})

app.mount('#app')
