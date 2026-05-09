<script setup lang="ts">
import { ref } from 'vue'
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
    okMsg.value = '密码已更新；为安全起见，所有会话已强制下线，请重新登录'
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
    <h2 class="text-2xl font-bold mb-6">账户与改密</h2>

    <div class="card mb-6 max-w-lg">
      <p class="text-sm text-gray-500">用户名</p>
      <p class="text-lg font-medium mt-1">{{ auth.user?.username || '—' }}</p>
      <p v-if="auth.user?.last_login_at" class="text-xs text-gray-500 mt-2">
        最近登录：{{ auth.user.last_login_at }}
      </p>
    </div>

    <div class="card max-w-lg">
      <h3 class="text-lg font-semibold mb-4">修改密码</h3>
      <form @submit.prevent="submit" class="space-y-4">
        <div>
          <label class="label">当前密码</label>
          <input v-model="oldPassword" type="password" class="input" autocomplete="current-password" />
        </div>
        <div>
          <label class="label">新密码（至少 8 位）</label>
          <input v-model="newPassword" type="password" class="input" autocomplete="new-password" />
        </div>
        <div>
          <label class="label">确认新密码</label>
          <input v-model="confirmPassword" type="password" class="input" autocomplete="new-password" />
        </div>
        <div v-if="errMsg" class="text-sm text-red-600">{{ errMsg }}</div>
        <div v-if="okMsg" class="text-sm text-emerald-700">{{ okMsg }}</div>
        <button type="submit" class="btn-primary w-full" :disabled="submitting">
          {{ submitting ? '提交中…' : '更新密码' }}
        </button>
      </form>
    </div>
  </div>
</template>
