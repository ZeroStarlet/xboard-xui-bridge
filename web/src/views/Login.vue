<script setup lang="ts">
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
  <div class="card w-full max-w-sm">
    <h1 class="text-2xl font-bold text-center mb-1">xboard-xui-bridge</h1>
    <p class="text-sm text-gray-500 text-center mb-6">管理面板登录</p>
    <form @submit.prevent="submit" class="space-y-4">
      <div>
        <label class="label">用户名</label>
        <input v-model="username" class="input" autocomplete="username" required />
      </div>
      <div>
        <label class="label">密码</label>
        <input v-model="password" type="password" class="input" autocomplete="current-password" required />
      </div>
      <div v-if="errorMsg" class="text-sm text-red-600">{{ errorMsg }}</div>
      <button type="submit" class="btn-primary w-full" :disabled="loading">
        {{ loading ? '登录中…' : '登录' }}
      </button>
    </form>
    <p class="mt-6 text-xs text-gray-500 text-center">
      首次启动密码会写入服务器日志与 <code class="bg-gray-100 px-1">data/initial_password.txt</code>
    </p>
  </div>
</template>
