<script setup lang="ts">
// 账户与改密页（v0.6 视觉重构 — shadcn-vue + i18n + 深色 + a11y）。
//
// 视觉策略：
//   - 双卡片布局：左侧"账户信息"卡（用户名 + 最近登录），右侧"修改密码"卡
//   - 用户名用大号字体显示，让"我是谁"一目了然
//   - 改密表单加密码强度指示器（基于长度），用 div 渐变进度条 + 文字标签
//   - 改密成功后倒计时 1.5s 跳转登录页（保留 v0.5 行为）
//   - 错误反馈走 inline Alert（与表单字段同区，更聚焦）
//
// i18n：所有 account.* / common.* 文案走 t()。
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Loader2, AlertCircle, UserRound, Lock } from 'lucide-vue-next'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { api } from '@/api/client'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()
const { toast } = useToast()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const errMsg = ref('')
const submitting = ref(false)

// 密码强度提示——纯客户端校验，与服务端"最低 8 位"规则一致。
// 不做复杂熵估算（强度判定本身就是个不完备问题），仅给运维一个
// 显性的"够 / 不够"信号。
const passwordStrength = computed<{
  labelKey: string
  tone: 'neutral' | 'danger' | 'warning' | 'success'
  percent: number
}>(() => {
  const v = newPassword.value
  if (!v) return { labelKey: '', tone: 'neutral', percent: 0 }
  if (v.length < 8) return { labelKey: 'account.strengthShort', tone: 'danger', percent: 25 }
  if (v.length < 12) return { labelKey: 'account.strengthFair', tone: 'warning', percent: 60 }
  if (v.length < 16) return { labelKey: 'account.strengthGood', tone: 'success', percent: 80 }
  return { labelKey: 'account.strengthExcellent', tone: 'success', percent: 100 }
})

async function submit() {
  errMsg.value = ''
  if (!oldPassword.value || !newPassword.value) {
    errMsg.value = t('account.errBothEmpty')
    return
  }
  if (newPassword.value.length < 8) {
    errMsg.value = t('account.errMinLength')
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    errMsg.value = t('account.errMismatch')
    return
  }
  submitting.value = true
  try {
    await api.changePassword(oldPassword.value, newPassword.value)
    toast({ title: t('account.okUpdated'), variant: 'success', duration: 2000 })
    // 后端已删 sessions——前端清空 store 并跳登录页。
    setTimeout(async () => {
      await auth.logout()
      router.push('/login')
    }, 1500)
  } catch (e) {
    void e
    errMsg.value = t('account.errFailed')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7">
      <h2 class="text-2xl font-semibold tracking-tight text-foreground">{{ t('account.title') }}</h2>
      <p class="mt-1 text-sm text-muted-foreground">{{ t('account.subtitle') }}</p>
    </header>

    <div class="grid grid-cols-1 gap-5 lg:grid-cols-3">
      <!-- 左：账户信息卡 -->
      <Card class="lg:col-span-1">
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
              <UserRound class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('account.profileTitle') }}</CardTitle>
              <CardDescription>{{ t('account.profileSubtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent class="space-y-4">
          <div>
            <p class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ t('account.fieldUsername') }}
            </p>
            <p class="mt-1 text-2xl font-semibold tracking-tight text-foreground">
              {{ auth.user?.username || t('common.dash') }}
            </p>
          </div>
          <div v-if="auth.user?.last_login_at" class="rounded-xl bg-muted/40 px-4 py-3">
            <p class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ t('account.fieldLastLogin') }}
            </p>
            <p class="mt-1 font-mono text-xs text-foreground">{{ auth.user.last_login_at }}</p>
          </div>
        </CardContent>
      </Card>

      <!-- 右：改密表单 -->
      <Card class="lg:col-span-2">
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
              <Lock class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('account.passwordTitle') }}</CardTitle>
              <CardDescription>{{ t('account.passwordSubtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <form class="space-y-5" novalidate @submit.prevent="submit">
            <div>
              <Label for="old-pwd">{{ t('account.fieldOldPwd') }}</Label>
              <Input
                id="old-pwd"
                v-model="oldPassword"
                type="password"
                autocomplete="current-password"
                class="mt-1.5"
              />
            </div>
            <div>
              <Label for="new-pwd">
                {{ t('account.fieldNewPwd') }}
                <span class="text-xs font-normal text-muted-foreground">{{ t('account.newPwdHelp') }}</span>
              </Label>
              <Input
                id="new-pwd"
                v-model="newPassword"
                type="password"
                autocomplete="new-password"
                class="mt-1.5"
              />
              <!-- 强度指示器：进度条 + 文字标签。空值时不显示。 -->
              <div v-if="newPassword" class="mt-2 flex items-center gap-3">
                <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
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
                    'text-rose-600 dark:text-rose-400': passwordStrength.tone === 'danger',
                    'text-amber-600 dark:text-amber-400': passwordStrength.tone === 'warning',
                    'text-brand-700 dark:text-brand-400': passwordStrength.tone === 'success',
                  }"
                >
                  {{ passwordStrength.labelKey ? t(passwordStrength.labelKey) : '' }}
                </span>
              </div>
            </div>
            <div>
              <Label for="confirm-pwd">{{ t('account.fieldConfirmPwd') }}</Label>
              <Input
                id="confirm-pwd"
                v-model="confirmPassword"
                type="password"
                autocomplete="new-password"
                class="mt-1.5"
              />
              <p
                v-if="confirmPassword && confirmPassword !== newPassword"
                class="mt-1.5 text-xs text-rose-600 dark:text-rose-400"
              >
                {{ t('account.errMismatchInline') }}
              </p>
            </div>

            <Alert v-if="errMsg" variant="destructive" role="alert" aria-live="assertive">
              <AlertCircle />
              <AlertDescription>{{ errMsg }}</AlertDescription>
            </Alert>

            <Button type="submit" class="w-full" :disabled="submitting" :aria-busy="submitting">
              <Loader2 v-if="submitting" class="animate-spin" aria-hidden="true" />
              {{ submitting ? t('account.submitting') : t('account.submit') }}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
