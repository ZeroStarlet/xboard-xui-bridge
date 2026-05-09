// Pinia 鉴权状态。
//
// 职责：
//
//   - 持有当前用户信息（user）+ 登录状态（initialized / isLoggedIn）；
//   - 暴露 fetchMe / login / logout 方法供视图调用；
//   - 路由守卫在每次切页面时调 fetchMe（仅未初始化时），避免每次 push
//     都打一次 /api/auth/me。
//
// 错误处理：方法在内部 try/catch，把 401 视为"未登录"清空 user；
// 其它错误向调用方抛出，由 view 决定提示文案。
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api, ApiError, type User } from '@/api/client'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const initialized = ref(false)

  const isLoggedIn = computed(() => user.value !== null)

  async function fetchMe() {
    try {
      const u = await api.me()
      user.value = u
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        user.value = null
      } else {
        // 其它错误（5xx / 网络）：保持 user 为 null，让 UI 提示运维。
        user.value = null
        // 仅在控制台留痕，不抛——避免登录页卡在 loading。
        console.warn('fetchMe failed:', e)
      }
    } finally {
      initialized.value = true
    }
  }

  async function login(username: string, password: string) {
    const u = await api.login(username, password)
    user.value = u
  }

  async function logout() {
    try {
      await api.logout()
    } catch (e) {
      // 后端 logout 失败不影响本地状态；至多控制台 warn。
      console.warn('logout failed:', e)
    }
    user.value = null
  }

  return { user, initialized, isLoggedIn, fetchMe, login, logout }
})
