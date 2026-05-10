<script setup lang="ts">
// 运行参数（v0.7 视觉重构 — Bento Live Console 步进式分组）。
//
// v0.7 与 v0.6 差异：
//
//   1. 信息架构：从"6 张并排卡片"改为"左侧锚点导航 + 右侧分组卡片"
//      步进式布局——左侧 nav 始终显示 6 个锚点（Xboard / 3x-ui /
//      同步周期 / 上报开关 / 日志 / Web）；右侧表单分组带 anchor id，
//      IntersectionObserver 监听可见性同步高亮 nav 当前项。
//
//   2. 顶部粘性 Save 条：与 LiveStatusBar 视觉同源（玻璃磁贴 + 粘性
//      top-12 紧贴状态条下方），实时显示 dirty 字段数量（"3 处改动
//      待保存"），让运维一眼看出"还没存的内容有几处"。
//
//   3. 锚点导航：与 AppNav 设计语言同源（左竖条 + 浅底高亮），让运维
//      从"页面间导航"过渡到"页面内分组导航"无认知断点。
//
// 数据流：
//
//   - dirty 计算走 computePatch()——只有真改动的字段才进 patch（与 v0.6
//     一致）。dirtyCount = Object.values(patch).flatMap(o => Object.keys o)
//     .length，用于 Save 条文案。
//
//   - 锚点跳转：scrollIntoView({ behavior: 'smooth', block: 'start' })，
//     主滚动容器是 main（document scroll），所以锚点滚动是窗口级。
//
// i18n：所有 settings.* / common.* 文案走 t()，新增 settings.anchor*
// + settings.savePending / allSaved。
import { ref, computed, onMounted, onUnmounted, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Loader2,
  Save,
  Globe,
  Server,
  Timer,
  Activity,
  FileText,
  Lock,
} from 'lucide-vue-next'
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

// canEdit / canSave：与 v0.6 等价——非加载中、非提交中、original 已加载。
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

// ============================================================
// 数据加载 / 保存
// ============================================================

async function refresh(): Promise<void> {
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

// dirty 字段数：所有 group 内改动字段总数。
const dirtyCount = computed<number>(() => {
  const patch = computePatch()
  let n = 0
  for (const g of Object.keys(patch) as (keyof SettingsPatch)[]) {
    const inner = patch[g] as Record<string, unknown> | undefined
    if (inner) n += Object.keys(inner).length
  }
  return n
})

async function submit(): Promise<void> {
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

// ============================================================
// 锚点导航
// ============================================================

interface AnchorEntry {
  id: string
  // labelKey 指向 i18n nav 标签
  labelKey: string
  icon: typeof Globe
}

const anchors: AnchorEntry[] = [
  { id: 'anchor-xboard',    labelKey: 'settings.anchorXboard',    icon: Globe },
  { id: 'anchor-xui',       labelKey: 'settings.anchorXui',       icon: Server },
  { id: 'anchor-intervals', labelKey: 'settings.anchorIntervals', icon: Timer },
  { id: 'anchor-reporting', labelKey: 'settings.anchorReporting', icon: Activity },
  { id: 'anchor-log',       labelKey: 'settings.anchorLog',       icon: FileText },
  { id: 'anchor-web',       labelKey: 'settings.anchorWeb',       icon: Lock },
]

const activeAnchor = ref<string>(anchors[0].id)

let observer: IntersectionObserver | null = null

/**
 * 设置 IntersectionObserver 监听各分组卡可见性。
 *
 * 触发时机：onMounted 在 refresh() 完成 + DOM 渲染完成后调用——但 refresh
 * 是异步的，DOM 在 form 填充后才完整。简单做法：在 onMounted 末尾延迟一帧
 * 用 nextTick 注册，或在 watch(form) 注册一次（但 form 多字段会重复触发）。
 *
 * 折中：onMounted 直接注册——anchor card 的 DOM 在 v-for 渲染时已存在
 * （锚点结构不依赖 form 数据，仅占位骨架就有 id），observer 注册是安全的。
 *
 * thresholds: [0.4]——分组占据视口 40% 以上时认为它"激活"。值过低会让
 * 滚动经过任意一像素就高亮跳动，过高又会让窄屏（小区域占视口）激活困难。
 *
 * rootMargin: 顶部偏移 60px——LiveStatusBar (h-12=48px) + Save 条
 * (h-12=48px) 大约 96px 的"已被遮挡"区，不算分组真正可见。这里取
 * '-60px 0px -40% 0px'：上 60px 不算可见；下 40% 视口剩余空间外不算。
 */
function setupObserver(): void {
  if (typeof IntersectionObserver === 'undefined') return
  observer = new IntersectionObserver(
    (entries) => {
      // 找到当前可见性最高的 entry，更新 activeAnchor。
      const visible = entries.filter((e) => e.isIntersecting)
      if (visible.length === 0) return
      // 多个同时可见时取最靠上的（boundingClientRect.top 最小）。
      visible.sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
      activeAnchor.value = visible[0].target.id
    },
    {
      threshold: [0.4],
      rootMargin: '-60px 0px -40% 0px',
    },
  )
  for (const a of anchors) {
    const el = document.getElementById(a.id)
    if (el) observer.observe(el)
  }
}

onMounted(() => {
  // 在下一帧注册——确保 v-for 锚点 DOM 已挂载。
  if (typeof window !== 'undefined') {
    window.requestAnimationFrame(setupObserver)
  }
})

onUnmounted(() => {
  observer?.disconnect()
  observer = null
})

/**
 * 平滑滚动到指定锚点。
 *
 * 顶部偏移走 CSS 变量 `--layout-stick-offset`（style.css :root 定义）——
 * 让"粘性顶部高度"作为单一来源，将来调整 LiveStatusBar / Save 条高度
 * 只改 CSS 一处，本视图自动跟随（v0.7 第 2 轮 Codex minor 反馈 #8）。
 * getComputedStyle().getPropertyValue 返回带空格的字符串（如 " 110px"），
 * parseFloat 容忍前导空白与单位后缀。
 *
 * "严禁兜底/回退/降级"——CSS 变量缺失或值无法解析视为开发期 bug：本函数
 * 直接 return 不滚动 + console.error 让缝隙在开发期立刻可见
 * （v0.7 第 3 轮 Codex nit 反馈 #2 严格化）。生产期 :root 必有
 * `--layout-stick-offset`，此分支不应被触达；若被触达说明 CSS 变量被
 * 重命名 / 删除，应当修 CSS 而非在 JS 端用 110 静默兜底掩盖。
 *
 * 行为偏好：尊重 prefers-reduced-motion——用户开启系统级减动画时
 * window.scrollTo 走 'auto'（瞬间跳转）而非 'smooth' 平滑动画
 * （v0.7 第 2 轮 Codex major 反馈 #4）。matchMedia 在每次调用时读取
 * 实时偏好，用户在系统设置切换后下次点锚点立即生效，无需刷新页面。
 */
function scrollToAnchor(id: string): void {
  const el = document.getElementById(id)
  if (!el) return
  const rawOffset = getComputedStyle(document.documentElement)
    .getPropertyValue('--layout-stick-offset')
  const topOffset = parseFloat(rawOffset)
  if (!Number.isFinite(topOffset)) {
    // CSS 变量缺失 / 解析失败——开发期立即可见的诊断，不静默兜底。
    console.error(
      'scrollToAnchor: CSS variable --layout-stick-offset missing or invalid (got %o); fix style.css :root.',
      rawOffset,
    )
    return
  }
  const top = el.getBoundingClientRect().top + window.scrollY - topOffset
  // 减动画偏好检测——Settings 视图仅在客户端渲染，window 必然存在；
  // typeof 守卫仅为未来被搬到 SSR 上下文时静默防御。
  const reduceMotion =
    typeof window !== 'undefined' &&
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  window.scrollTo({ top, behavior: reduceMotion ? 'auto' : 'smooth' })
}
</script>

<template>
  <div>
    <!-- ============================================================
         页面头
         ============================================================ -->
    <header class="mb-5">
      <h2 class="text-2xl font-semibold tracking-tight text-foreground">
        {{ t('settings.title') }}
      </h2>
      <p class="mt-1 text-sm text-muted-foreground">{{ t('settings.subtitle') }}</p>
    </header>

    <!-- ============================================================
         主区：左锚点 nav（lg+ 才显示）+ 右表单滚动区
         ============================================================ -->
    <div class="flex gap-6">
      <!-- 锚点导航：sticky top-24 紧贴状态条 + Save 条下方。
           窄屏 (< lg) 隐藏——表单本身分组卡片已自带头部图标，无需重复
           占侧栏空间；用户用滚动条 / 浏览器查找直接定位。 -->
      <aside class="sticky top-24 hidden h-fit w-48 shrink-0 lg:block">
        <nav class="space-y-0.5" :aria-label="t('settings.anchorNavAria')">
          <button
            v-for="a in anchors"
            :key="a.id"
            type="button"
            class="anchor-nav-item w-full"
            :class="{ 'anchor-nav-item-active': activeAnchor === a.id }"
            @click="scrollToAnchor(a.id)"
          >
            <component :is="a.icon" class="size-4 shrink-0" aria-hidden="true" />
            <span class="flex-1 text-left">{{ t(a.labelKey) }}</span>
          </button>
        </nav>
      </aside>

      <!-- 右侧表单 -->
      <div class="flex-1 min-w-0 space-y-5">
        <!-- 顶部粘性 Save 条 —— 玻璃磁贴 + dirty 计数 + 操作按钮 -->
        <div class="glass-tile sticky top-14 z-30 flex items-center justify-between px-5 py-3">
          <div class="flex items-center gap-2 text-sm">
            <Save class="size-4 text-muted-foreground" aria-hidden="true" />
            <span v-if="dirtyCount === 0" class="text-muted-foreground">
              {{ t('settings.allSaved') }}
            </span>
            <span v-else class="font-medium text-foreground">
              {{ t('settings.savePending', { count: dirtyCount }) }}
            </span>
          </div>
          <div class="flex items-center gap-2">
            <Button variant="ghost" size="sm" :disabled="loading" @click="refresh">
              {{ t('common.refresh') }}
            </Button>
            <Button size="sm" :disabled="!canSave || dirtyCount === 0" :aria-busy="submitting" @click="submit">
              <Loader2 v-if="submitting" class="animate-spin" aria-hidden="true" />
              <Save v-else aria-hidden="true" />
              {{ submitting ? t('common.saving') : t('common.save') }}
            </Button>
          </div>
        </div>

        <fieldset :disabled="!canEdit" class="contents space-y-5">
          <div class="space-y-5">
            <!-- ========== Xboard ========== -->
            <section id="anchor-xboard" class="bento-tile scroll-mt-28">
              <header class="mb-4 flex items-center gap-3">
                <span
                  class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
                  aria-hidden="true"
                >
                  <Globe class="h-5 w-5" />
                </span>
                <div class="flex-1">
                  <h3 class="text-base font-semibold text-foreground">{{ t('settings.xboard.title') }}</h3>
                  <p class="text-xs text-muted-foreground">{{ t('settings.xboard.subtitle') }}</p>
                </div>
              </header>
              <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
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
              </div>
            </section>

            <!-- ========== 3x-ui ========== -->
            <section id="anchor-xui" class="bento-tile scroll-mt-28">
              <header class="mb-4 flex items-center gap-3">
                <span
                  class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
                  aria-hidden="true"
                >
                  <Server class="h-5 w-5" />
                </span>
                <div class="flex-1">
                  <h3 class="text-base font-semibold text-foreground">{{ t('settings.xui.title') }}</h3>
                  <p class="text-xs text-muted-foreground">{{ t('settings.xui.subtitle') }}</p>
                </div>
              </header>

              <Alert variant="info" role="status" class="mb-5">
                <AlertDescription>{{ t('settings.xui.infoBanner') }}</AlertDescription>
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
            </section>

            <!-- ========== 同步周期 ========== -->
            <section id="anchor-intervals" class="bento-tile scroll-mt-28">
              <header class="mb-4 flex items-center gap-3">
                <span
                  class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
                  aria-hidden="true"
                >
                  <Timer class="h-5 w-5" />
                </span>
                <div class="flex-1">
                  <h3 class="text-base font-semibold text-foreground">{{ t('settings.intervals.title') }}</h3>
                  <p class="text-xs text-muted-foreground">{{ t('settings.intervals.subtitle') }}</p>
                </div>
              </header>
              <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
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
              </div>
            </section>

            <!-- ========== 上报开关 ========== -->
            <section id="anchor-reporting" class="bento-tile scroll-mt-28">
              <header class="mb-4 flex items-center gap-3">
                <span
                  class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
                  aria-hidden="true"
                >
                  <Activity class="h-5 w-5" />
                </span>
                <div class="flex-1">
                  <h3 class="text-base font-semibold text-foreground">{{ t('settings.reporting.title') }}</h3>
                  <p class="text-xs text-muted-foreground">{{ t('settings.reporting.subtitle') }}</p>
                </div>
              </header>
              <div class="space-y-3">
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
              </div>
            </section>

            <!-- ========== 日志 ========== -->
            <section id="anchor-log" class="bento-tile scroll-mt-28">
              <header class="mb-4 flex items-center gap-3">
                <span
                  class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
                  aria-hidden="true"
                >
                  <FileText class="h-5 w-5" />
                </span>
                <div class="flex-1">
                  <h3 class="text-base font-semibold text-foreground">{{ t('settings.log.title') }}</h3>
                  <p class="text-xs text-muted-foreground">{{ t('settings.log.subtitle') }}</p>
                </div>
              </header>
              <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
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
              </div>
            </section>

            <!-- ========== Web（只读） ========== -->
            <section
              id="anchor-web"
              class="bento-tile scroll-mt-28 border-amber-200 bg-amber-50/30 dark:border-amber-800 dark:bg-amber-950/20"
            >
              <header class="mb-4 flex items-center gap-3">
                <span
                  class="flex h-9 w-9 items-center justify-center rounded-xl bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
                  aria-hidden="true"
                >
                  <Lock class="h-5 w-5" />
                </span>
                <div class="flex-1">
                  <h3 class="text-base font-semibold text-foreground">{{ t('settings.web.title') }}</h3>
                  <p class="text-xs text-amber-700 dark:text-amber-400">{{ t('settings.web.subtitle') }}</p>
                </div>
              </header>

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
            </section>
          </div>
        </fieldset>
      </div>
    </div>
  </div>
</template>
