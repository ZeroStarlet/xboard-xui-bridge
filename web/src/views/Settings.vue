<script setup lang="ts">
// 运行参数（v0.6 视觉重构 — shadcn-vue + i18n + 深色 + a11y）。
//
// 信息架构：
//   - 顶部标题 + 操作区（刷新 / 保存）
//   - 6 个独立 Card：Xboard / 3x-ui / 同步周期 / 上报开关 / 日志 / Web 只读
//   - 每个 Card 头部：图标徽章 + 标题 + 副标题（描述用途）
//   - Web 卡片用琥珀色边框 + 内部说明，与可编辑组视觉区分
//
// 视觉迁移：
//   - .input → Input + Label
//   - .cb → Switch（reporting toggles）/ Checkbox（skip_tls_verify）
//   - .alert-error / .alert-success → toast 非阻塞通知
//   - .label / .help-text → Label + 内嵌 muted-foreground 段落
//   - <select> → shadcn-vue Select（仅 log level）
//
// dirty diff patch 逻辑保留（避免重写整个 settings 表）：computePatch 比较
// form 与 original，仅把改动字段放入 PATCH 请求体。
//
// i18n：所有 settings.* / common.* / errors.* 文案走 t()。
import { ref, computed, onMounted, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Loader2,
  RefreshCw,
  Save,
  Globe,
  Server,
  Timer,
  Activity,
  FileText,
  Lock,
} from 'lucide-vue-next'
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
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/select'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useToast } from '@/composables/useToast'
import { api, type Settings, type SettingsPatch } from '@/api/client'

const { t } = useI18n()
const { toast } = useToast()

const loading = ref(true)
const submitting = ref(false)

const original = ref<Settings | null>(null)

// canSave 派生：仅在配置已加载、不在保存中、不在刷新中时允许保存。
//
// 修复 v0.6 初版的交互 bug（批次 10 Codex 第 1 轮指出）：
//   - 初始加载中：original=null，computePatch 返回 {} → 点击 Save 会看到
//     误导的"无改动" toast；
//   - 加载失败：同 above，且用户输入也无意义；
//   - 加载慢期间用户输入：refresh() 完成时 Object.assign() 把 form 覆盖，
//     用户改动丢失。
//
// 现在禁用按钮 + 输入框（disabled）让用户清楚"加载未完成不能编辑"，体验
// 与传统表单加载一致。同时 submitting 期间也禁用——避免用户在 patchSettings
// 请求飞行中继续修改字段，等请求成功后的 refresh() 把新输入覆盖掉
// （批次 10 Codex 第 2 轮指出的"保存中输入丢失"场景）。
//
// canEdit 与 canSave 当前条件等价（!loading && !submitting && original 已加载），
// 保留两个名字纯为可读性：模板 fieldset :disabled="!canEdit" 表达"能否编辑"，
// 顶部 Save 按钮 :disabled="!canSave" 表达"能否点击保存"，语义不同但
// 当前判定逻辑相同。
const canEdit = computed(() =>
  !loading.value && !submitting.value && original.value !== null,
)
const canSave = canEdit
const form = reactive({
  log: { level: 'info', file: '', max_size_mb: 0, max_backups: 0, max_age_days: 0 },
  xboard: { api_host: '', token: '', timeout_sec: 15, skip_tls_verify: false, user_agent: '' },
  xui: {
    api_host: '',
    base_path: '',
    api_token: '',
    timeout_sec: 15,
    skip_tls_verify: false,
  },
  intervals: { user_pull_sec: 60, traffic_push_sec: 60, alive_push_sec: 60, status_push_sec: 60 },
  reporting: { alive_enabled: false, status_enabled: false },
  web: { listen_addr: '', session_max_age_hours: 0, absolute_max_lifetime_hours: 0 },
})

async function refresh() {
  loading.value = true
  try {
    const s = await api.getSettings()
    original.value = s
    Object.assign(form.log, s.log)
    Object.assign(form.xboard, s.xboard)
    Object.assign(form.xui, s.xui)
    Object.assign(form.intervals, s.intervals)
    Object.assign(form.reporting, s.reporting)
    Object.assign(form.web, s.web)
  } catch (e) {
    void e
    toast({ title: t('settings.errLoadFailed'), variant: 'destructive' })
  } finally {
    loading.value = false
  }
}

// 计算 patch：仅当字段值与 original 不同时才放入 patch 中。
// 避免把"当前值"全量重写到 store，减少 reload 触发面。
// 与 v0.5 实现完全一致——只是把错误显示从内嵌 alert 改为 toast。
function computePatch(): SettingsPatch {
  if (!original.value) return {}
  const patch: SettingsPatch = {}
  const groups: (keyof Settings)[] = ['log', 'xboard', 'xui', 'intervals', 'reporting']
  for (const g of groups) {
    const cur = form[g] as Record<string, unknown>
    const orig = original.value[g] as Record<string, unknown>
    const diff: Record<string, unknown> = {}
    for (const k of Object.keys(cur)) {
      if (cur[k] !== orig[k]) diff[k] = cur[k]
    }
    if (Object.keys(diff).length > 0) {
      ;(patch as Record<string, unknown>)[g] = diff
    }
  }
  return patch
}

async function submit() {
  const patch = computePatch()
  if (Object.keys(patch).length === 0) {
    toast({ title: t('settings.errNoChanges'), variant: 'warning' })
    return
  }
  submitting.value = true
  try {
    await api.patchSettings(patch)
    toast({ title: t('settings.okSaved'), variant: 'success' })
    await refresh()
  } catch (e) {
    void e
    toast({ title: t('errors.saveFailed'), variant: 'destructive' })
  } finally {
    submitting.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7 flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-foreground">{{ t('settings.title') }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('settings.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <Button variant="outline" :disabled="loading" @click="refresh">
          <Loader2 v-if="loading" class="animate-spin" aria-hidden="true" />
          <RefreshCw v-else aria-hidden="true" />
          {{ t('common.refresh') }}
        </Button>
        <Button :disabled="!canSave" :aria-busy="submitting" @click="submit">
          <Loader2 v-if="submitting" class="animate-spin" aria-hidden="true" />
          <Save v-else aria-hidden="true" />
          {{ submitting ? t('common.saving') : t('common.save') }}
        </Button>
      </div>
    </header>

    <!--
      fieldset disabled 让加载未完成时所有内部 input/select/button 自动禁用——
      原生 fieldset 行为，无需给每个控件单独传 :disabled。class="contents" 让
      fieldset 自身不参与 flex/grid 布局（display: contents），与原 div 包裹
      行为视觉一致。
      用 :disabled="!canEdit" 而非 disabled 字面属性：Vue 编译时会按响应式更新。
    -->
    <fieldset :disabled="!canEdit" class="contents space-y-5">
      <div class="space-y-5">
      <!-- Xboard -->
      <Card>
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
              <Globe class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('settings.xboard.title') }}</CardTitle>
              <CardDescription>{{ t('settings.xboard.subtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <Label for="s-xboard-api_host">{{ t('settings.xboard.fieldHost') }}</Label>
            <Input id="s-xboard-api_host" v-model="form.xboard.api_host" :placeholder="t('settings.xboard.hostPlaceholder')" class="mt-1.5" />
          </div>
          <div>
            <Label for="s-xboard-token">{{ t('settings.xboard.fieldToken') }}</Label>
            <Input id="s-xboard-token" v-model="form.xboard.token" type="password" autocomplete="off" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-xboard-timeout_sec">{{ t('settings.xboard.fieldTimeout') }}</Label>
            <Input id="s-xboard-timeout_sec" v-model.number="form.xboard.timeout_sec" type="number" min="1" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-xboard-user_agent">{{ t('settings.xboard.fieldUserAgent') }}</Label>
            <Input id="s-xboard-user_agent" v-model="form.xboard.user_agent" class="mt-1.5" />
          </div>
          <div class="md:col-span-2 flex items-center gap-3">
            <Checkbox id="s-xboard-skip_tls_verify" v-model="form.xboard.skip_tls_verify" />
            <Label for="s-xboard-skip_tls_verify" class="cursor-pointer">
              {{ t('settings.xboard.skipTlsLabel') }}
              <span class="text-xs font-normal text-muted-foreground">{{ t('settings.xboard.skipTlsHelp') }}</span>
            </Label>
          </div>
        </CardContent>
      </Card>

      <!-- 3x-ui -->
      <Card>
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
              <Server class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('settings.xui.title') }}</CardTitle>
              <CardDescription>{{ t('settings.xui.subtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <Alert variant="info" role="status" class="mb-5">
            <AlertDescription>
              {{ t('settings.xui.infoBanner') }}
            </AlertDescription>
          </Alert>
          <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
            <div>
              <Label for="s-xui-api_host">{{ t('settings.xui.fieldHost') }}</Label>
              <Input id="s-xui-api_host" v-model="form.xui.api_host" :placeholder="t('settings.xui.hostPlaceholder')" class="mt-1.5" />
            </div>
            <div>
              <Label for="s-xui-base_path">{{ t('settings.xui.fieldBasePath') }}</Label>
              <Input id="s-xui-base_path" v-model="form.xui.base_path" :placeholder="t('settings.xui.basePathPlaceholder')" class="mt-1.5" />
            </div>
            <div class="md:col-span-2">
              <Label for="s-xui-api_token">{{ t('settings.xui.fieldApiToken') }}</Label>
              <Input
                id="s-xui-api_token"
                v-model="form.xui.api_token"
                type="password"
                autocomplete="off"
                :placeholder="t('settings.xui.apiTokenPlaceholder')"
                class="mt-1.5 font-mono"
              />
              <p class="mt-1.5 text-xs text-muted-foreground">{{ t('settings.xui.apiTokenHelp') }}</p>
            </div>
            <div>
              <Label for="s-xui-timeout_sec">{{ t('settings.xui.fieldTimeout') }}</Label>
              <Input id="s-xui-timeout_sec" v-model.number="form.xui.timeout_sec" type="number" min="1" class="mt-1.5 font-mono" />
            </div>
            <div class="md:col-span-2 flex items-center gap-3">
              <Checkbox id="s-xui-skip_tls_verify" v-model="form.xui.skip_tls_verify" />
              <Label for="s-xui-skip_tls_verify" class="cursor-pointer">{{ t('settings.xui.skipTlsLabel') }}</Label>
            </div>
          </div>
        </CardContent>
      </Card>

      <!-- 同步周期 -->
      <Card>
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
              <Timer class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('settings.intervals.title') }}</CardTitle>
              <CardDescription>{{ t('settings.intervals.subtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent class="grid grid-cols-2 gap-4 md:grid-cols-4">
          <div>
            <Label for="s-int-user_pull_sec">{{ t('settings.intervals.userPull') }}</Label>
            <Input id="s-int-user_pull_sec" v-model.number="form.intervals.user_pull_sec" type="number" min="5" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-int-traffic_push_sec">{{ t('settings.intervals.trafficPush') }}</Label>
            <Input id="s-int-traffic_push_sec" v-model.number="form.intervals.traffic_push_sec" type="number" min="5" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-int-alive_push_sec">{{ t('settings.intervals.alivePush') }}</Label>
            <Input id="s-int-alive_push_sec" v-model.number="form.intervals.alive_push_sec" type="number" min="5" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-int-status_push_sec">{{ t('settings.intervals.statusPush') }}</Label>
            <Input id="s-int-status_push_sec" v-model.number="form.intervals.status_push_sec" type="number" min="5" class="mt-1.5 font-mono" />
          </div>
        </CardContent>
      </Card>

      <!-- 上报开关 -->
      <Card>
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
              <Activity class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('settings.reporting.title') }}</CardTitle>
              <CardDescription>{{ t('settings.reporting.subtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent class="space-y-3">
          <!-- alive 上报：用 Switch 而非 Checkbox 表达"立即生效"语义 -->
          <div class="flex items-start justify-between gap-4 rounded-xl border p-4">
            <div class="flex-1">
              <Label for="s-rep-alive_enabled" class="cursor-pointer text-sm font-medium text-foreground">
                {{ t('settings.reporting.aliveTitle') }}
              </Label>
              <p class="mt-0.5 text-xs text-muted-foreground">{{ t('settings.reporting.aliveDesc') }}</p>
            </div>
            <Switch id="s-rep-alive_enabled" v-model="form.reporting.alive_enabled" />
          </div>
          <div class="flex items-start justify-between gap-4 rounded-xl border p-4">
            <div class="flex-1">
              <Label for="s-rep-status_enabled" class="cursor-pointer text-sm font-medium text-foreground">
                {{ t('settings.reporting.statusTitle') }}
              </Label>
              <p class="mt-0.5 text-xs text-muted-foreground">{{ t('settings.reporting.statusDesc') }}</p>
            </div>
            <Switch id="s-rep-status_enabled" v-model="form.reporting.status_enabled" />
          </div>
        </CardContent>
      </Card>

      <!-- 日志 -->
      <Card>
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
              <FileText class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('settings.log.title') }}</CardTitle>
              <CardDescription>{{ t('settings.log.subtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <Label for="s-log-level">{{ t('settings.log.fieldLevel') }}</Label>
            <Select v-model="form.log.level">
              <SelectTrigger id="s-log-level" class="mt-1.5">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="debug">debug</SelectItem>
                <SelectItem value="info">info</SelectItem>
                <SelectItem value="warn">warn</SelectItem>
                <SelectItem value="error">error</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div>
            <Label for="s-log-file">
              {{ t('settings.log.fieldFile') }}
              <span class="text-xs font-normal text-muted-foreground">{{ t('settings.log.fieldFileHelp') }}</span>
            </Label>
            <Input id="s-log-file" v-model="form.log.file" :placeholder="t('settings.log.filePlaceholder')" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-log-max_size_mb">{{ t('settings.log.fieldMaxSize') }}</Label>
            <Input id="s-log-max_size_mb" v-model.number="form.log.max_size_mb" type="number" min="0" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-log-max_backups">{{ t('settings.log.fieldMaxBackups') }}</Label>
            <Input id="s-log-max_backups" v-model.number="form.log.max_backups" type="number" min="0" class="mt-1.5 font-mono" />
          </div>
          <div>
            <Label for="s-log-max_age_days">{{ t('settings.log.fieldMaxAge') }}</Label>
            <Input id="s-log-max_age_days" v-model.number="form.log.max_age_days" type="number" min="0" class="mt-1.5 font-mono" />
          </div>
        </CardContent>
      </Card>

      <!-- Web（只读） -->
      <Card class="border-amber-200 bg-amber-50/30 dark:border-amber-800 dark:bg-amber-950/20">
        <CardHeader>
          <div class="flex items-center gap-3">
            <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400" aria-hidden="true">
              <Lock class="h-5 w-5" />
            </span>
            <div class="flex-1">
              <CardTitle>{{ t('settings.web.title') }}</CardTitle>
              <CardDescription class="text-amber-700 dark:text-amber-400">{{ t('settings.web.subtitle') }}</CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <Alert variant="warning" role="status" class="mb-5">
            <AlertDescription>
              <span>{{ t('settings.web.tipPrefix') }}</span>
              <code class="rounded bg-amber-100 px-1.5 py-0.5 font-mono dark:bg-amber-900/40">
                {{ t('settings.web.tipCmdChange') }}
              </code>
              <span>{{ t('settings.web.tipMid') }}</span>
              <code class="rounded bg-amber-100 px-1.5 py-0.5 font-mono dark:bg-amber-900/40">
                {{ t('settings.web.tipCmdSqlite') }}
              </code>
              <span>{{ t('settings.web.tipSuffix') }}</span>
            </AlertDescription>
          </Alert>
          <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
            <div>
              <Label for="s-web-listen_addr">{{ t('settings.web.fieldListenAddr') }}</Label>
              <Input id="s-web-listen_addr" :model-value="form.web.listen_addr" disabled class="mt-1.5 font-mono" />
            </div>
            <div>
              <Label for="s-web-session_max_age_hours">{{ t('settings.web.fieldSessionMaxAge') }}</Label>
              <Input id="s-web-session_max_age_hours" :model-value="form.web.session_max_age_hours" disabled class="mt-1.5 font-mono" />
            </div>
            <div>
              <Label for="s-web-absolute_max_lifetime_hours">{{ t('settings.web.fieldAbsoluteMax') }}</Label>
              <Input id="s-web-absolute_max_lifetime_hours" :model-value="form.web.absolute_max_lifetime_hours" disabled class="mt-1.5 font-mono" />
            </div>
          </div>
        </CardContent>
      </Card>
      </div>
    </fieldset>
  </div>
</template>
