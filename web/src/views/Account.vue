<script setup lang="ts">
// 账户与改密页（v0.7 视觉重构 — Bento Live Console）。
//
// v0.7 与 v0.6 差异：
//
//   1. Hero 头像区：超大圆形渐变头像（与 LiveStatusBar 用户头像同源
//      色系——emerald → blue 渐变）+ 用户名 hero 字（display-num
//      4xl），让"我是谁"页面的视觉锚点立刻被识别。
//
//   2. 改密表单：玻璃磁贴（.glass-tile）替代默认 Card——让 aurora 背景
//      透过来，与 Login 页"玻璃 + 极光"调性呼应；改密流程视觉上自带
//      "敏感操作"权重。
//
//   3. 强度指示器：进度条 + 文字颜色双线索保留（v0.6 实现），但底色改
//      为 muted 灰，让强度色（rose / amber / brand）单独承担视觉对比。
//
// 业务逻辑：与 v0.6 完全一致——非空校验 + 长度 + 一致性 + 1.5s 跳转登录。
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Loader2, AlertCircle, Lock, ShieldCheck } from 'lucide-vue-next'
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

// 用户名首字母——头像里展示。
const initial = computed<string>(() => {
  const u = auth.user?.username ?? ''
  return u.slice(0, 1).toUpperCase() || '?'
})

// 密码强度——与 v0.6 等价（长度阈值 8 / 12 / 16）。
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

async function submit(): Promise<void> {
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
  <div class="space-y-5">
    <!-- 页面头 -->
    <header>
      <h2 class="text-2xl font-semibold tracking-tight text-foreground">{{ t('account.title') }}</h2>
      <p class="mt-1 text-sm text-muted-foreground">{{ t('account.subtitle') }}</p>
    </header>

    <!-- ============================================================
         Hero 头像卡：大头像 + 用户名 hero 字 + 副信息
         占满全宽，gradient bg + spotlight shadow——与"运维就是你"的
         个人专属感呼应。
         ============================================================ -->
    <section class="bento-tile-hero">
      <div class="flex flex-col items-start gap-5 sm:flex-row sm:items-center">
        <!-- 大头像：圆形渐变 + 首字母居中 + soft shadow + 内描边 -->
        <div
          class="flex h-20 w-20 shrink-0 items-center justify-center rounded-full text-3xl font-semibold text-white shadow-soft"
          style="background: linear-gradient(135deg, #10b981, #3b82f6);"
          aria-hidden="true"
        >
          {{ initial }}
        </div>

        <div class="flex-1 min-w-0">
          <p class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t('account.fieldUsername') }}
          </p>
          <p class="mt-1 display-num truncate text-3xl">
            {{ auth.user?.username || t('common.dash') }}
          </p>
          <p
            v-if="auth.user?.last_login_at"
            class="mt-2 inline-flex items-center gap-1.5 text-xs text-muted-foreground"
          >
            <ShieldCheck class="size-3.5" aria-hidden="true" />
            <span>{{ t('account.fieldLastLogin') }}</span>
            <span class="font-mono">{{ auth.user.last_login_at }}</span>
          </p>
        </div>
      </div>
    </section>

    <!-- ============================================================
         改密表单：玻璃磁贴
         ============================================================ -->
    <section class="glass-tile p-6">
      <header class="mb-5 flex items-center gap-3">
        <span
          class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
          aria-hidden="true"
        >
          <Lock class="h-5 w-5" />
        </span>
        <div class="flex-1">
          <h3 class="text-base font-semibold text-foreground">{{ t('account.passwordTitle') }}</h3>
          <p class="text-xs text-muted-foreground">{{ t('account.passwordSubtitle') }}</p>
        </div>
      </header>

      <form class="grid grid-cols-1 gap-5 md:grid-cols-2" novalidate @submit.prevent="submit">
        <div class="md:col-span-2">
          <Label for="old-pwd">{{ t('account.fieldOldPwd') }}</Label>
          <Input
            id="old-pwd"
            v-model="oldPassword"
            type="password"
            autocomplete="current-password"
            class="mt-1.5"
          />
        </div>

        <div class="md:col-span-1">
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
          <!-- 强度指示器（与 v0.6 同结构） -->
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

        <div class="md:col-span-1">
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

        <div class="md:col-span-2">
          <Alert v-if="errMsg" variant="destructive" role="alert" aria-live="assertive">
            <AlertCircle />
            <AlertDescription>{{ errMsg }}</AlertDescription>
          </Alert>
        </div>

        <div class="md:col-span-2">
          <Button type="submit" class="w-full" :disabled="submitting" :aria-busy="submitting">
            <Loader2 v-if="submitting" class="animate-spin" aria-hidden="true" />
            {{ submitting ? t('account.submitting') : t('account.submit') }}
          </Button>
        </div>
      </form>
    </section>
  </div>
</template>
