<script setup lang="ts">
// 登录页（v0.5 视觉重构）。
//
// 视觉策略：
//   - 玻璃拟态卡片（card-glass）—— 半透明 + 模糊背景，让 App.vue 渲染的渐变光斑
//     透过卡片视觉若隐若现，营造"高端"感。
//   - 顶部渐变图标——用 SVG 内嵌 + linearGradient 让 logo 也有渐变品牌色，
//     与导航栏的渐变文字呼应。
//   - 输入框焦点态用 ring-glow（绿色弥散光）—— 区别于浏览器默认蓝色焦点环，
//     让用户感知"这是我们的设计"。
//   - 按钮 loading 态用旋转 spinner——比纯文字"登录中…"更专业。
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ApiError } from '@/api/client'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const username = ref('admin')
const password = ref('')
const errorMsg = ref('')
const loading = ref(false)

async function submit() {
  errorMsg.value = ''
  if (!username.value || !password.value) {
    errorMsg.value = '请输入用户名与密码'
    return
  }
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    const redirect = (route.query.redirect as string) || '/dashboard'
    router.push(redirect)
  } catch (e) {
    if (e instanceof ApiError) {
      errorMsg.value = e.message
    } else {
      errorMsg.value = '登录失败，请检查网络'
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="card-glass">
    <!-- Logo + 标题 -->
    <div class="mb-8 flex flex-col items-center text-center">
      <!-- 渐变 logo 图标：圆角方形 + 闪电纹饰，意指"中间件 / 桥接"。 -->
      <div class="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl shadow-soft"
           style="background: linear-gradient(135deg, #10b981, #3b82f6);">
        <svg
          class="h-7 w-7 text-white"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"
          aria-hidden="true"
        >
          <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
        </svg>
      </div>
      <h1 class="text-xl font-semibold tracking-tight text-surface-900">
        <span class="text-gradient-brand">xboard-xui-bridge</span>
      </h1>
      <p class="mt-1.5 text-sm text-surface-500">管理面板登录</p>
    </div>

    <!-- 表单 -->
    <form @submit.prevent="submit" class="space-y-5">
      <div>
        <label class="label" for="login-username">用户名</label>
        <input
          id="login-username"
          v-model="username"
          class="input"
          autocomplete="username"
          required
          autofocus
        />
      </div>
      <div>
        <label class="label" for="login-password">密码</label>
        <input
          id="login-password"
          v-model="password"
          type="password"
          class="input"
          autocomplete="current-password"
          required
        />
      </div>

      <!-- 错误提示——使用 alert-error 横幅，与全局警示视觉风格一致。 -->
      <div v-if="errorMsg" class="alert-error">
        <svg
          class="h-5 w-5 shrink-0"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="2"
          aria-hidden="true"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z"
          />
        </svg>
        <span>{{ errorMsg }}</span>
      </div>

      <button type="submit" class="btn-primary w-full justify-center" :disabled="loading">
        <!-- loading 态用旋转图标 + 文字，比纯文字更专业。 -->
        <svg
          v-if="loading"
          class="h-4 w-4 animate-spin"
          fill="none"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path
            class="opacity-75"
            fill="currentColor"
            d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
          />
        </svg>
        <span>{{ loading ? '登录中…' : '登录' }}</span>
      </button>
    </form>

    <!-- 首次启动提示 -->
    <div class="mt-7 rounded-xl border border-surface-200 bg-surface-50/80 px-4 py-3">
      <p class="text-xs leading-relaxed text-surface-600">
        <span class="font-medium text-surface-700">首次登录提示：</span>
        密码会写入服务器日志与
        <code class="rounded bg-surface-200 px-1.5 py-0.5 font-mono text-[11px] text-surface-700">
          data/initial_password.txt
        </code>
        ；登录后请立即修改并妥善保管。
      </p>
    </div>
  </div>
</template>
