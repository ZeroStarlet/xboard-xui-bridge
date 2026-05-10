<script setup lang="ts">
// 登录页（v0.7 视觉重构 — Bento Live Console 玻璃登录卡 + Aurora 极光）。
//
// 视觉策略：
//   - 玻璃拟态卡片（card-glass）—— v0.5/0.6 视觉资产保留：半透明 + 模糊背景。
//     v0.7 起 App.vue 把登录页背景升级为 .aurora-bg 流动极光（取代 v0.6
//     的多层静态光斑），玻璃卡片透出缓慢呼吸的极光——比静态背景更有
//     "进入 Live Console 仪表"的仪式感。
//   - 渐变 logo 图标—— v0.7 加 animate-breathe（4s 周期 0.5% scale 振幅）
//     让 logo 有"活气"感，不再是冷冰冰的静态品牌；振幅极小，前庭敏感
//     用户也无察觉负担（reduced-motion 下走 CSS @media 自动停用）。
//   - 输入控件用 shadcn-vue Input + Label 组件，焦点环用 ring 语义 token，
//     深色模式 ring-offset 跟随 background。
//   - 按钮 loading 态用旋转 spinner（lucide Loader2），比纯文字更专业。
//
// i18n：所有文案走 t()，包括 aria-label、placeholder、错误提示。
//
// 可访问性（WCAG AA 强化）：
//   - 错误横幅（Alert）role="alert" + aria-live="assertive" 让屏幕阅读器
//     立刻播报错误内容
//   - 按钮 loading 时 aria-busy + aria-live="polite" 让"登录中…"被朗读
//   - autofocus 在用户名输入框，避免每次进入页面都需要鼠标点击
//   - autocomplete 属性正确设置（username / current-password），让浏览器
//     密码管理器与 1Password 等 helper 自动填充
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter, useRoute } from 'vue-router'
import { Loader2, Zap, AlertCircle } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const username = ref('admin')
const password = ref('')
const errorMsg = ref('')
const loading = ref(false)

// loading button 的 aria 属性派生——避免在模板里写多个三元表达式。
const submitButtonLabel = computed(() =>
  loading.value ? t('login.submitting') : t('login.submit'),
)

async function submit() {
  errorMsg.value = ''
  if (!username.value || !password.value) {
    errorMsg.value = t('login.errEmpty')
    return
  }
  loading.value = true
  try {
    await auth.login(username.value, password.value)
    const redirect = (route.query.redirect as string) || '/dashboard'
    router.push(redirect)
  } catch (e) {
    // 错误显示策略：v0.5 把 ApiError.message（后端原始中文）直接显示给用户，
    // 但英文界面就会看到非本地化中文（"用户名或密码错误"等）。v0.6 起统一
    // 走 t('login.errFailed') 让显示语言与界面 locale 一致；代价是损失服务端
    // 给出的具体错误码（"密码错误" vs "用户被锁定"）的细颗粒度反馈。
    //
    // 后续改进路径：在 zh-CN/en-US locale 里按 e.code 映射不同 key
    // （e.g. login.errInvalidCreds / login.errRateLimited），让英文用户也
    // 能看到具体原因。短期内（本批次）保持简单——任何登录失败都显示同一
    // 通用文案，与现有后端"登录失败 401"主流场景吻合。
    void e
    errorMsg.value = t('login.errFailed')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="card-glass">
    <!-- Logo + 标题 -->
    <div class="mb-8 flex flex-col items-center text-center">
      <!-- 渐变 logo 图标：圆角方形 + 闪电纹饰，意指"中间件 / 桥接"。
           lucide Zap 图标替代 v0.5 的内联 SVG path，源码更干净。 -->
      <div class="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl shadow-soft animate-breathe"
           style="background: linear-gradient(135deg, #10b981, #3b82f6);">
        <Zap class="h-7 w-7 text-white" stroke-width="2" aria-hidden="true" />
      </div>
      <h1 class="text-xl font-semibold tracking-tight text-foreground">
        <span class="text-gradient-brand">xboard-xui-bridge</span>
      </h1>
      <p class="mt-1.5 text-sm text-muted-foreground">{{ t('login.title') }}</p>
    </div>

    <!-- 表单 -->
    <form @submit.prevent="submit" class="space-y-5" novalidate>
      <div>
        <Label for="login-username">{{ t('login.username') }}</Label>
        <Input
          id="login-username"
          v-model="username"
          autocomplete="username"
          required
          autofocus
          class="mt-1.5"
        />
      </div>
      <div>
        <Label for="login-password">{{ t('login.password') }}</Label>
        <Input
          id="login-password"
          v-model="password"
          type="password"
          autocomplete="current-password"
          required
          class="mt-1.5"
        />
      </div>

      <!--
        错误提示：用 shadcn-vue Alert（destructive variant）+ role="alert" +
        aria-live="assertive"，让屏幕阅读器在错误出现时立刻播报。
        v-if 让 alert 仅在错误存在时挂载，避免 aria-live 在空文本上反复触发。
      -->
      <Alert v-if="errorMsg" variant="destructive" role="alert" aria-live="assertive">
        <AlertCircle />
        <AlertDescription>{{ errorMsg }}</AlertDescription>
      </Alert>

      <Button
        type="submit"
        class="w-full"
        :disabled="loading"
        :aria-busy="loading"
        aria-live="polite"
      >
        <Loader2 v-if="loading" class="animate-spin" aria-hidden="true" />
        <span>{{ submitButtonLabel }}</span>
      </Button>
    </form>

    <!--
      首次启动提示：用三段式拼接（prefix + code + suffix）让文件名独立做
      <code> 高亮；i18n 三段 key 与 zh-CN.json / en-US.json 对齐。
    -->
    <div class="mt-7 rounded-xl border bg-muted/40 px-4 py-3">
      <p class="text-xs leading-relaxed text-muted-foreground">
        <span class="font-medium text-foreground">{{ t('login.firstTimeHint') }}</span>
        <span>{{ t('login.firstTimeBodyPrefix') }}</span>
        <code class="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground">
          {{ t('login.firstTimeFile') }}
        </code>
        <span>{{ t('login.firstTimeBodySuffix') }}</span>
      </p>
    </div>
  </div>
</template>
