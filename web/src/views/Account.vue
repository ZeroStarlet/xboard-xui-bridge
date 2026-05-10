<script setup lang="ts">
// 账户与改密页（v0.5 视觉重构）。
//
// 视觉策略：
//   - 双卡片布局：左侧"账户信息"展示卡（用户名 + 最近登录时间），右侧
//     "修改密码"操作卡——左查右改的工作流符合习惯。
//   - 用户信息卡顶部有渐变图标徽章 + 大号字体显示用户名，让"我是谁"
//     一目了然。
//   - 改密表单加密码强度指示器（简单：长度判定）—— 用户能即时看到
//     新密码是否达到 8 位最低要求。
//   - 改密成功后倒计时 1.5 秒跳转登录页，倒计时显示在按钮上。
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { api, ApiError } from '@/api/client'

const auth = useAuthStore()
const router = useRouter()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const errMsg = ref('')
const okMsg = ref('')
const submitting = ref(false)

// 密码强度提示——纯客户端校验，与服务端的"最低 8 位"规则一致。
// 不做复杂熵估算（强度判定本身就是个不完备问题），仅给运维一个
// 显性的"够 / 不够"信号。
const passwordStrength = computed(() => {
  const v = newPassword.value
  if (!v) return { label: '', tone: 'neutral', percent: 0 }
  if (v.length < 8) return { label: '太短', tone: 'danger', percent: 25 }
  if (v.length < 12) return { label: '一般', tone: 'warning', percent: 60 }
  if (v.length < 16) return { label: '良好', tone: 'success', percent: 80 }
  return { label: '极佳', tone: 'success', percent: 100 }
})

async function submit() {
  errMsg.value = ''
  okMsg.value = ''
  if (!oldPassword.value || !newPassword.value) {
    errMsg.value = '旧密码与新密码不可为空'
    return
  }
  if (newPassword.value.length < 8) {
    errMsg.value = '新密码长度至少 8 位'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    errMsg.value = '两次输入的新密码不一致'
    return
  }
  submitting.value = true
  try {
    await api.changePassword(oldPassword.value, newPassword.value)
    okMsg.value = '密码已更新；为安全起见，所有会话已强制下线，1.5 秒后跳转登录页…'
    // 后端已删 sessions——前端清空 store 并跳登录页。
    setTimeout(async () => {
      await auth.logout()
      router.push('/login')
    }, 1500)
  } catch (e) {
    errMsg.value = e instanceof ApiError ? e.message : '改密失败'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7">
      <h2 class="text-2xl font-semibold tracking-tight text-surface-900">账户与改密</h2>
      <p class="mt-1 text-sm text-surface-500">查看当前管理员信息并修改登录密码</p>
    </header>

    <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
      <!-- 左：账户信息卡 -->
      <section class="card lg:col-span-1">
        <header class="section-title">
          <span class="section-title-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0A17.933 17.933 0 0112 21.75c-2.676 0-5.216-.584-7.499-1.632z" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">账户信息</h3>
            <p class="section-title-subtitle">当前登录的管理员</p>
          </div>
        </header>

        <div class="space-y-4">
          <div>
            <p class="kpi-label">用户名</p>
            <p class="mt-1 text-2xl font-semibold tracking-tight text-surface-900">
              {{ auth.user?.username || '—' }}
            </p>
          </div>
          <div v-if="auth.user?.last_login_at" class="rounded-xl bg-surface-50 px-4 py-3">
            <p class="kpi-label">最近登录</p>
            <p class="mt-1 font-mono text-xs text-surface-700">{{ auth.user.last_login_at }}</p>
          </div>
        </div>
      </section>

      <!-- 右：改密表单 -->
      <section class="card lg:col-span-2">
        <header class="section-title">
          <span class="section-title-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">修改密码</h3>
            <p class="section-title-subtitle">改密后所有会话强制下线，本次会话也会跳转登录页</p>
          </div>
        </header>

        <form @submit.prevent="submit" class="space-y-5">
          <div>
            <label class="label" for="old-pwd">当前密码</label>
            <input
              id="old-pwd"
              v-model="oldPassword"
              type="password"
              class="input"
              autocomplete="current-password"
            />
          </div>
          <div>
            <label class="label" for="new-pwd">新密码 <span class="text-xs font-normal text-surface-500">（至少 8 位）</span></label>
            <input
              id="new-pwd"
              v-model="newPassword"
              type="password"
              class="input"
              autocomplete="new-password"
            />
            <!-- 强度指示器：进度条 + 文字标签。空值时不显示。 -->
            <div v-if="newPassword" class="mt-2 flex items-center gap-3">
              <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-surface-200">
                <div
                  class="h-full rounded-full transition-all duration-300"
                  :class="{
                    'bg-rose-500': passwordStrength.tone === 'danger',
                    'bg-amber-500': passwordStrength.tone === 'warning',
                    'bg-brand-500': passwordStrength.tone === 'success',
                  }"
                  :style="{ width: passwordStrength.percent + '%' }"
                />
              </div>
              <span
                class="text-xs font-medium"
                :class="{
                  'text-rose-600': passwordStrength.tone === 'danger',
                  'text-amber-700': passwordStrength.tone === 'warning',
                  'text-brand-700': passwordStrength.tone === 'success',
                }"
              >
                {{ passwordStrength.label }}
              </span>
            </div>
          </div>
          <div>
            <label class="label" for="confirm-pwd">确认新密码</label>
            <input
              id="confirm-pwd"
              v-model="confirmPassword"
              type="password"
              class="input"
              autocomplete="new-password"
            />
            <p
              v-if="confirmPassword && confirmPassword !== newPassword"
              class="help-text text-rose-600"
            >
              两次输入不一致
            </p>
          </div>

          <div v-if="errMsg" class="alert-error">
            <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
            </svg>
            <span>{{ errMsg }}</span>
          </div>
          <div v-if="okMsg" class="alert-success">
            <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>{{ okMsg }}</span>
          </div>

          <button type="submit" class="btn-primary w-full justify-center" :disabled="submitting">
            <svg
              v-if="submitting"
              class="h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
            </svg>
            {{ submitting ? '提交中…' : '更新密码' }}
          </button>
        </form>
      </section>
    </div>
  </div>
</template>
