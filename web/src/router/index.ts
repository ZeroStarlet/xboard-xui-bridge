// Vue Router 配置。
//
// 路由布局：
//
//   /login            登录页（layout=auth，无导航栏）
//   /dashboard        仪表盘（layout=panel，主入口）
//   /bridges          桥接管理
//   /settings         运行参数
//   /account          账户与改密
//   /                 默认重定向 → /dashboard
//
// 全局守卫：
//
//   - 未登录访问 panel 路由 → 跳 /login
//   - 已登录访问 /login → 跳 /dashboard（避免登录后回退到登录页）
//
// 鉴权状态判定：调 /api/auth/me 拿用户信息；存在即认为已登录。
// 这一调用由 auth store 统一缓存，路由守卫每次切页面时只在状态未初始化
// 时触发一次。
import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory('/'),
  routes: [
    {
      path: '/',
      redirect: '/dashboard',
    },
    {
      path: '/login',
      name: 'login',
      meta: { layout: 'auth' },
      component: () => import('@/views/Login.vue'),
    },
    {
      path: '/dashboard',
      name: 'dashboard',
      meta: { layout: 'panel', requiresAuth: true },
      component: () => import('@/views/Dashboard.vue'),
    },
    {
      path: '/bridges',
      name: 'bridges',
      meta: { layout: 'panel', requiresAuth: true },
      component: () => import('@/views/Bridges.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      meta: { layout: 'panel', requiresAuth: true },
      component: () => import('@/views/Settings.vue'),
    },
    {
      path: '/account',
      name: 'account',
      meta: { layout: 'panel', requiresAuth: true },
      component: () => import('@/views/Account.vue'),
    },
    {
      // 兜底：未匹配路径 → 仪表盘（也可以做 404 页）。
      path: '/:pathMatch(.*)*',
      redirect: '/dashboard',
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.initialized) {
    await auth.fetchMe()
  }
  if (to.meta.requiresAuth && !auth.isLoggedIn) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  if (to.path === '/login' && auth.isLoggedIn) {
    return { path: '/dashboard' }
  }
  return true
})

export default router
