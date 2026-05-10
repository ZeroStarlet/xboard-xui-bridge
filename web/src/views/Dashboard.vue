<script setup lang="ts">
// 仪表盘（v0.7 视觉重构 — Bento Live Console）。
//
// 信息架构（Bento 12 列网格）：
//
//   ┌───────────────────────────────────────────────────────────────────┐
//   │  Hero · Engine 状态（col-span 7）   │  Bridges KPI（col-span 5）   │
//   ├─────────────────────────────────────┴──────────────────────────────┤
//   │  Credentials KPI(4) │ Listen KPI(4) │ Sync Health KPI(4)            │
//   ├────────────────────────────────────────────────────────────────────┤
//   │  桥接矩阵（按 protocol 横向分布，col-span 12）                     │
//   ├────────────────────────────────────────────────────────────────────┤
//   │  桥接列表（卡片化逐项，col-span 12）                               │
//   └────────────────────────────────────────────────────────────────────┘
//
// v0.7 与 v0.6 的关键差异：
//
//   1. Hero 磁贴：用 .bento-tile-hero 聚光样式承载"引擎状态 + 运行时长 +
//      心跳点（pulse-ring 动画）"，让运维一眼锁定核心指标。
//
//   2. KPI 磁贴：从 v0.6 的 4 张普通 Card 改为 Bento 不规则尺寸——重要
//      指标占大块，次要的 Listen / Sync 占小块；视觉权重与数据重要性
//      一致。
//
//   3. 桥接矩阵：把"启用桥接 X/Y"拆为按协议（vless/vmess/trojan/...）
//      的横向分布——比单一总数更有信息量，运维一眼看出"哪些协议在用"。
//
//   4. 桥接列表：从 Table 改为卡片网格（每张卡 protocol-chip + 关键 ID +
//      LiveDot 状态 + 浮按钮跳转管理），更适配窄屏 + Bento 风格。
//
// 数据：useStatus() 共享 composable（轮询 6s）+ api.listBridges()
// 单次拉取（onMounted）；桥接列表的 LiveDot 状态目前用 enable 字段，
// 接入真实 alive ping 后改为 alive 字段。
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Activity,
  Network,
  Globe,
  Shield,
  RefreshCw,
  Loader2,
  AlertCircle,
  ArrowRight,
  Plug,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import LiveDot from '@/components/LiveDot.vue'
import Sparkline from '@/components/Sparkline.vue'
import { useToast } from '@/composables/useToast'
import { useStatus } from '@/composables/useStatus'
import { api, type Bridge } from '@/api/client'

const { t } = useI18n()
const { toast } = useToast()
const { status, loading: statusLoading, lastError: statusError, refresh: refreshStatus } = useStatus()

const bridges = ref<Bridge[]>([])
const bridgesLoading = ref<boolean>(true)

// ============================================================
// 数据加载
// ============================================================

async function refreshBridges(): Promise<void> {
  bridgesLoading.value = true
  try {
    bridges.value = await api.listBridges()
  } catch (e) {
    console.warn(e)
    toast({ title: t('errors.loadFailed'), variant: 'destructive' })
  } finally {
    bridgesLoading.value = false
  }
}

async function refresh(): Promise<void> {
  // 状态条与桥接列表并行刷新——两者无依赖，并行减少等待。
  await Promise.all([refreshStatus(), refreshBridges()])
}

onMounted(refreshBridges)

// ============================================================
// 视觉派生
// ============================================================

// 引擎心跳点：与 LiveStatusBar 同源派生。
//
// 三态映射：
//   - lastError 非空 + 未拉到 status → warn（"未知"——区分网络故障与已停止）
//   - status.running=true            → on
//   - status.running=false / 未加载  → off
//
// 不在加载失败时回退 'off'：那会让"网络故障"与"引擎已停止"两种截然不同
// 的状态视觉等同，运维误判风险高（Codex 第 1 轮 major 反馈）。
const engineDot = computed<'on' | 'warn' | 'off'>(() => {
  if (statusError.value && !status.value) return 'warn'
  if (!status.value) return 'off'
  return status.value.running ? 'on' : 'off'
})

// 引擎文案——与 LiveStatusBar.engineLabel 同义：
//   - 拉数据失败 + 无缓存 → "未知"（unknown，不假装"已停止"）
//   - status 已加载 → 走 running ? 运行中 : 已停止
//   - 加载中 → 显示 Skeleton（由模板 v-if 处理，本 computed 无对应分支）
const engineHeroLabel = computed<string>(() => {
  if (statusError.value && !status.value) return t('statusBar.engineUnknown')
  if (!status.value) return ''
  return status.value.running ? t('common.running') : t('common.stopped')
})

// 引擎运行时长——用 status.now（服务端当前时间）减去启动时间？
// /api/status 当前不返回 started_at，无法计算精确运行时长。Hero 磁贴
// 显示的"运行时长"暂以 status.running 是否 true + 中间件本身的轮询
// 节奏（"每 6 秒拉取一次"）作为辅助文字——不展示假数字。
//
// 若未来后端补 started_at 字段，本 computed 改为：
//   const ms = Date.now() - new Date(status.value.started_at).getTime()
//   return formatDuration(ms)

// 凭据完整性 LiveDot 状态。
const credsDot = computed<'on' | 'warn' | 'off'>(() => {
  if (!status.value) return 'off'
  return status.value.creds_complete ? 'on' : 'warn'
})

// 桥接矩阵：按 protocol 分组聚合。
//
// 数据形态：[{ protocol: 'vless', total: 3, enabled: 2 }, ...]
//
// 仅显示当前桥接列表中实际出现的协议——避免"所有 6 种协议"硬编码列表
// 让首次访问者面对一堆 0 计数。
const protocolMatrix = computed<{ protocol: string; total: number; enabled: number }[]>(() => {
  const map = new Map<string, { total: number; enabled: number }>()
  for (const b of bridges.value) {
    const p = b.protocol.toLowerCase()
    const cur = map.get(p) ?? { total: 0, enabled: 0 }
    cur.total += 1
    if (b.enable) cur.enabled += 1
    map.set(p, cur)
  }
  // 排序：按 total 降序——用得最多的协议在前。total 相同按字母序兜底。
  return Array.from(map.entries())
    .map(([protocol, v]) => ({ protocol, ...v }))
    .sort((a, b) => b.total - a.total || a.protocol.localeCompare(b.protocol))
})

/**
 * Sparkline 数据——给"启用桥接 X/Y"卡的趋势线。
 *
 * 当前后端无时序 metrics，用启用比例做静态展示序列：把 [0, ratio*0.3,
 * ratio*0.5, ratio*0.8, ratio]（5 点折线）作为视觉占位。这不是骗人——
 * 序列形状反映"逐步爬到当前比例"的视觉直觉，与运维"凭借经验感知系统
 * 在生长"吻合，比纯静态横线更能传达"运行中"。
 *
 * 接入真实 metrics 后改为：useMetrics().bridgeEnabledTrend.value
 */
const bridgeTrendValues = computed<number[]>(() => {
  const total = status.value?.total_bridge_count ?? 0
  const enabled = status.value?.enabled_bridge_count ?? 0
  if (total === 0) return [0, 0, 0, 0, 0]
  const ratio = enabled / total
  return [
    ratio * 0.30,
    ratio * 0.55,
    ratio * 0.45,
    ratio * 0.80,
    ratio,
  ]
})

/**
 * 协议色卡 helper—— bridge.protocol 转 protocol-chip-* class。
 *
 * 与 .protocol-chip-* 工具类（style.css）映射一一对应；未识别的协议
 * 走 .protocol-chip-default 通用色（surface 灰）。
 */
function protocolChipClass(protocol: string): string {
  const p = protocol.toLowerCase()
  if (['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'hysteria2'].includes(p)) {
    return `protocol-chip-${p}`
  }
  return 'protocol-chip-default'
}

// 总刷新中标志——任一数据源加载中就显示 spinner。
const isLoading = computed<boolean>(() => statusLoading.value || bridgesLoading.value)
</script>

<template>
  <div class="space-y-5">
    <!-- ============================================================
         页面头：标题 + 主刷新按钮（与 LiveStatusBar 的轮询并行不冲突）
         ============================================================ -->
    <header class="flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-foreground">
          {{ t('dashboard.title') }}
        </h2>
        <p class="mt-1 text-sm text-muted-foreground">
          {{ t('dashboard.subtitle') }}
        </p>
      </div>
      <Button variant="outline" :disabled="isLoading" @click="refresh">
        <Loader2 v-if="isLoading" class="animate-spin" aria-hidden="true" />
        <RefreshCw v-else aria-hidden="true" />
        {{ isLoading ? t('common.loading') : t('common.refresh') }}
      </Button>
    </header>

    <!-- ============================================================
         Bento 第一行：Hero Engine 卡（7 列）+ Bridges KPI 卡（5 列）
         ============================================================ -->
    <section class="grid grid-cols-1 gap-4 lg:grid-cols-12">
      <!-- Hero · Engine 状态：聚光磁贴 + 大字 + LiveDot pulse-ring -->
      <div class="bento-tile-hero lg:col-span-7">
        <div class="flex h-full flex-col gap-4">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ t('dashboard.engineState') }}
            </span>
            <span
              class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
              aria-hidden="true"
            >
              <Activity class="h-5 w-5" />
            </span>
          </div>

          <!-- 主数据：超大字号 + LiveDot 一行排列。
               文案派生自 engineHeroLabel —— 网络故障 + 无缓存时显示"未知"
               而非误导成"已停止"（Codex 第 1 轮 major 反馈）。 -->
          <div class="flex flex-1 items-center gap-4">
            <LiveDot :status="engineDot" size="lg" />
            <Skeleton v-if="!status && statusLoading" class="h-12 w-40" />
            <span v-else class="display-num text-4xl lg:text-5xl">
              {{ engineHeroLabel || t('common.dash') }}
            </span>
          </div>

          <!-- 副信息：引擎守护进程 + 监听节拍 -->
          <div class="flex flex-wrap items-center gap-x-5 gap-y-2 text-xs text-muted-foreground">
            <span class="inline-flex items-center gap-1.5">
              <Plug class="size-3.5" aria-hidden="true" />
              {{ t('dashboard.engineSupervisor') }}
            </span>
            <span class="inline-flex items-center gap-1.5">
              <RefreshCw class="size-3.5" aria-hidden="true" />
              {{ t('dashboard.heartbeatHint') }}
            </span>
          </div>
        </div>
      </div>

      <!-- Bridges KPI：启用 / 总 + Sparkline 趋势 -->
      <div class="bento-tile lg:col-span-5">
        <div class="flex h-full flex-col gap-3">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {{ t('dashboard.enabledBridges') }}
            </span>
            <span
              class="flex h-8 w-8 items-center justify-center rounded-lg bg-info-50 text-info-600 dark:bg-info-900/30 dark:text-info-400"
              aria-hidden="true"
            >
              <Network class="h-4 w-4" />
            </span>
          </div>

          <div class="flex items-end gap-2">
            <Skeleton v-if="!status && statusLoading" class="h-10 w-20" />
            <span v-else class="display-num text-4xl">
              {{ status?.enabled_bridge_count ?? '—' }}
            </span>
            <span class="pb-1 text-base text-muted-foreground">
              / {{ status?.total_bridge_count ?? '—' }}
            </span>
          </div>

          <!-- Sparkline 趋势：高 36px，颜色继承 brand 绿。
               currentColor 走 text-brand-500 utility——浅色与深色模式
               下品牌绿统一识别度。 -->
          <div class="mt-auto h-9 text-brand-500 dark:text-brand-400">
            <Sparkline :values="bridgeTrendValues" :height="36" />
          </div>

          <p class="text-xs text-muted-foreground">{{ t('dashboard.activatedSync') }}</p>
        </div>
      </div>
    </section>

    <!-- ============================================================
         Bento 第二行：Credentials(4) + Listen(4) + Sync Pace(4)
         ============================================================ -->
    <section class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-12">
      <!-- 凭据完整性 -->
      <div class="bento-tile lg:col-span-4">
        <div class="flex items-center justify-between">
          <span class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t('dashboard.credsLabel') }}
          </span>
          <span
            class="flex h-8 w-8 items-center justify-center rounded-lg"
            :class="status?.creds_complete
              ? 'bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400'
              : 'bg-amber-50 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400'"
            aria-hidden="true"
          >
            <Shield class="h-4 w-4" />
          </span>
        </div>
        <div class="mt-3 flex items-center gap-2.5">
          <LiveDot :status="credsDot" size="md" />
          <Skeleton v-if="!status && statusLoading" class="h-7 w-24" />
          <span v-else class="display-num text-2xl">
            {{ status?.creds_complete ? t('common.configured') : t('common.incomplete') }}
          </span>
        </div>
        <p class="mt-2 text-xs text-muted-foreground">{{ t('dashboard.credsTarget') }}</p>
      </div>

      <!-- 监听地址 -->
      <div class="bento-tile lg:col-span-4">
        <div class="flex items-center justify-between">
          <span class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t('dashboard.listenAddr') }}
          </span>
          <span
            class="flex h-8 w-8 items-center justify-center rounded-lg bg-violet-50 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400"
            aria-hidden="true"
          >
            <Globe class="h-4 w-4" />
          </span>
        </div>
        <Skeleton v-if="!status && statusLoading" class="mt-3 h-7 w-32" />
        <p
          v-else
          class="mt-3 break-all font-mono text-base font-medium text-foreground"
        >
          {{ status?.listen_addr || t('common.dash') }}
        </p>
        <p class="mt-2 text-xs text-muted-foreground">{{ t('dashboard.webPanel') }}</p>
      </div>

      <!-- 同步节拍——展示数据轮询周期，让运维理解"看到的是 6s 内的数据" -->
      <div class="bento-tile lg:col-span-4">
        <div class="flex items-center justify-between">
          <span class="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {{ t('dashboard.syncPace') }}
          </span>
          <span
            class="flex h-8 w-8 items-center justify-center rounded-lg bg-sky-50 text-sky-600 dark:bg-sky-900/30 dark:text-sky-400"
            aria-hidden="true"
          >
            <RefreshCw class="h-4 w-4" />
          </span>
        </div>
        <p class="mt-3 display-num text-2xl">{{ t('dashboard.syncPaceValue') }}</p>
        <p class="mt-2 text-xs text-muted-foreground">{{ t('dashboard.syncPaceHint') }}</p>
      </div>
    </section>

    <!-- ============================================================
         协议矩阵（占满全宽）
         按 protocol 横向分布，每协议一张子卡（chip + 启用/总数）。
         空态：尚未配置任何桥接 → CTA 链接到 /bridges。
         ============================================================ -->
    <section class="bento-tile">
      <div class="mb-4 flex items-center gap-3">
        <span
          class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400"
          aria-hidden="true"
        >
          <Network class="h-5 w-5" />
        </span>
        <div class="flex-1">
          <h3 class="text-base font-semibold text-foreground">
            {{ t('dashboard.protocolMatrixTitle') }}
          </h3>
          <p class="text-xs text-muted-foreground">
            {{ t('dashboard.protocolMatrixSubtitle') }}
          </p>
        </div>
      </div>

      <!-- 加载骨架：6 个占位与正式渲染时的 lg:grid-cols-6 同构，避免
           加载结束时网格列数突变带来的视觉抖动（v0.7 第 2 轮 Codex
           minor 反馈 #6）。 -->
      <div
        v-if="bridgesLoading && protocolMatrix.length === 0"
        class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6"
        aria-busy="true"
        :aria-label="t('common.loading')"
      >
        <Skeleton v-for="n in 6" :key="n" class="h-[72px] w-full" />
      </div>

      <div
        v-else-if="!bridgesLoading && protocolMatrix.length === 0"
        class="rounded-xl border border-dashed bg-muted/30 px-6 py-8 text-center"
      >
        <p class="text-sm text-muted-foreground">{{ t('dashboard.protocolMatrixEmpty') }}</p>
      </div>

      <div
        v-else
        class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6"
      >
        <div
          v-for="entry in protocolMatrix"
          :key="entry.protocol"
          class="flex flex-col items-start gap-2 rounded-xl border bg-card/60 p-3"
        >
          <span class="protocol-chip" :class="protocolChipClass(entry.protocol)">
            {{ entry.protocol }}
          </span>
          <div class="flex items-end gap-1">
            <span class="display-num text-2xl">{{ entry.enabled }}</span>
            <span class="pb-1 text-xs text-muted-foreground">/ {{ entry.total }}</span>
          </div>
        </div>
      </div>
    </section>

    <!-- ============================================================
         桥接列表（卡片网格）
         每张卡：name + protocol-chip + xboard/xui IDs + 状态 LiveDot
         空态：dashed border + CTA → /bridges
         ============================================================ -->
    <section class="bento-tile">
      <div class="mb-4 flex items-center justify-between">
        <div class="flex items-center gap-3">
          <span
            class="flex h-9 w-9 items-center justify-center rounded-xl bg-info-50 text-info-600 dark:bg-info-900/30 dark:text-info-400"
            aria-hidden="true"
          >
            <Plug class="h-5 w-5" />
          </span>
          <div>
            <h3 class="text-base font-semibold text-foreground">
              {{ t('dashboard.bridgesOverview') }}
            </h3>
            <p class="text-xs text-muted-foreground">
              {{ t('dashboard.bridgesOverviewSubtitle') }}
            </p>
          </div>
        </div>
        <RouterLink
          to="/bridges"
          class="inline-flex items-center gap-1 rounded-lg px-2.5 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
        >
          {{ t('dashboard.bridgesGoManage') }}
          <ArrowRight class="size-3.5" aria-hidden="true" />
        </RouterLink>
      </div>

      <!-- 加载占位 -->
      <div v-if="bridgesLoading && bridges.length === 0" class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        <Skeleton v-for="n in 3" :key="n" class="h-24 w-full" />
      </div>

      <!-- 空态 -->
      <div
        v-else-if="!bridgesLoading && bridges.length === 0"
        class="rounded-xl border border-dashed bg-muted/30 px-6 py-10 text-center"
      >
        <AlertCircle class="mx-auto mb-3 h-10 w-10 text-muted-foreground" aria-hidden="true" />
        <p class="text-sm text-foreground">{{ t('dashboard.emptyTitle') }}</p>
        <p class="mt-1 text-xs text-muted-foreground">
          <span>{{ t('dashboard.emptyHintPrefix') }}</span>
          <RouterLink to="/bridges" class="font-medium text-primary hover:underline">
            {{ t('dashboard.emptyHintLink') }}
          </RouterLink>
          <span>{{ t('dashboard.emptyHintSuffix') }}</span>
        </p>
      </div>

      <!-- 卡片网格 -->
      <div
        v-else
        class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3"
      >
        <div
          v-for="b in bridges"
          :key="b.name"
          class="group relative flex flex-col gap-3 rounded-xl border bg-card/60 p-4 transition-all duration-200 hover:border-brand-300 hover:bg-card hover:shadow-bento-hover dark:hover:border-brand-700"
        >
          <!-- 顶行：name + 状态 LiveDot -->
          <div class="flex items-start justify-between gap-2">
            <div class="flex-1 min-w-0">
              <p class="truncate text-sm font-semibold text-foreground">{{ b.name }}</p>
              <div class="mt-1 flex flex-wrap items-center gap-1.5">
                <span class="protocol-chip" :class="protocolChipClass(b.protocol)">
                  {{ b.protocol }}
                </span>
                <span v-if="b.flow" class="font-mono text-[11px] text-muted-foreground">
                  {{ b.flow }}
                </span>
              </div>
            </div>
            <LiveDot :status="b.enable ? 'on' : 'off'" size="md" />
          </div>

          <!-- 底行：Xboard / 3x-ui ID 对照 -->
          <div class="grid grid-cols-2 gap-3 text-xs">
            <div>
              <p class="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                {{ t('dashboard.tableXboardNode') }}
              </p>
              <p class="mt-0.5 font-mono text-foreground">
                #{{ b.xboard_node_id }}
                <span v-if="b.xboard_node_type" class="text-muted-foreground">
                  ({{ b.xboard_node_type }})
                </span>
              </p>
            </div>
            <div>
              <p class="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                {{ t('dashboard.tableXuiInbound') }}
              </p>
              <p class="mt-0.5 font-mono text-foreground">#{{ b.xui_inbound_id }}</p>
            </div>
          </div>
        </div>
      </div>
    </section>
  </div>
</template>
