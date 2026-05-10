<script setup lang="ts">
// 仪表盘（v0.6 视觉重构 — shadcn-vue + i18n + 深色 + a11y）。
//
// 信息架构：
//   - 顶部页面标题 + 操作区（刷新按钮）
//   - KPI 区：4 张大数据卡（引擎状态 / 桥接 / 凭据 / 监听）—— 每张卡片用
//     Card + Badge 组件而非 v0.5 的自实现 .pill-* 类，深色模式自动语义切换
//   - 桥接概览表：用 shadcn-vue Table 家族替代 v0.5 的 .data-table
//
// 视觉细节：
//   - KPI 卡的图标徽章使用与状态匹配的色调（运行中=绿，未完整=琥珀，等等）
//   - 加载态用 Skeleton 占位，比"加载中…"文字更专业
//   - 错误反馈走 toast 而非内嵌 alert（非阻塞，不打断浏览节奏）
//
// i18n：所有文案走 t()，包括表头 / 状态标签 / 空态提示。
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshCw, Loader2, Play, Network, Shield, Globe, AlertCircle } from 'lucide-vue-next'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
import { useToast } from '@/composables/useToast'
import { api, type Status, type Bridge } from '@/api/client'

const { t } = useI18n()
const { toast } = useToast()

const status = ref<Status | null>(null)
const bridges = ref<Bridge[]>([])
const loading = ref(true)

async function refresh() {
  loading.value = true
  try {
    const [s, bs] = await Promise.all([api.getStatus(), api.listBridges()])
    status.value = s
    bridges.value = bs
  } catch (e) {
    // toast 替代 v0.5 的内嵌 errMsg alert——不打断当前页面布局，让运维
    // 看到红角通知就够了，无需保留错误横幅占位。
    console.warn(e)
    toast({
      title: t('errors.loadFailed'),
      variant: 'destructive',
    })
  } finally {
    loading.value = false
  }
}

// 派生：根据 status 字段计算各 KPI 的视觉状态（Badge variant）。
//
// Badge variant 映射：
//   running=true → success（brand 绿）
//   running=false → destructive（rose 红）
//   未加载（null）→ secondary（灰）
//
// 用 Badge variant 而非 v0.5 的 .pill-success / .pill-danger 等类，让深色
// 模式自动跟随 token 切换，无需在视图里 if-else dark: 修饰符。
const engineBadge = computed<{ variant: 'success' | 'destructive' | 'secondary'; labelKey: string }>(() => {
  if (!status.value) return { variant: 'secondary', labelKey: 'common.dash' }
  return status.value.running
    ? { variant: 'success', labelKey: 'common.running' }
    : { variant: 'destructive', labelKey: 'common.stopped' }
})

const credsBadge = computed<{ variant: 'success' | 'warning' | 'secondary'; labelKey: string }>(() => {
  if (!status.value) return { variant: 'secondary', labelKey: 'common.dash' }
  return status.value.creds_complete
    ? { variant: 'success', labelKey: 'common.configured' }
    : { variant: 'warning', labelKey: 'common.incomplete' }
})

onMounted(refresh)
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7 flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-foreground">{{ t('dashboard.title') }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('dashboard.subtitle') }}</p>
      </div>
      <Button variant="outline" :disabled="loading" @click="refresh">
        <Loader2 v-if="loading" class="animate-spin" aria-hidden="true" />
        <RefreshCw v-else aria-hidden="true" />
        {{ loading ? t('common.loading') : t('common.refresh') }}
      </Button>
    </header>

    <!-- KPI 网格 -->
    <section class="mb-7 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <!-- 引擎状态卡 -->
      <Card>
        <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-2">
          <CardDescription class="uppercase tracking-wider">{{ t('dashboard.engineState') }}</CardDescription>
          <span
            class="flex h-8 w-8 items-center justify-center rounded-lg"
            :class="status?.running ? 'bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400' : 'bg-rose-50 text-rose-600 dark:bg-rose-900/30 dark:text-rose-400'"
            aria-hidden="true"
          >
            <Play class="h-4 w-4" />
          </span>
        </CardHeader>
        <CardContent class="pt-2">
          <Skeleton v-if="loading && !status" class="h-9 w-24" />
          <p v-else class="text-3xl font-semibold tracking-tight text-foreground">
            {{ t(engineBadge.labelKey) }}
          </p>
          <Badge :variant="engineBadge.variant" class="mt-2">
            <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
            {{ t('dashboard.engineSupervisor') }}
          </Badge>
        </CardContent>
      </Card>

      <!-- 桥接卡 -->
      <Card>
        <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-2">
          <CardDescription class="uppercase tracking-wider">{{ t('dashboard.enabledBridges') }}</CardDescription>
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-info-50 text-info-600 dark:bg-info-900/30 dark:text-info-400" aria-hidden="true">
            <Network class="h-4 w-4" />
          </span>
        </CardHeader>
        <CardContent class="pt-2">
          <Skeleton v-if="loading && !status" class="h-9 w-24" />
          <p v-else class="text-3xl font-semibold tracking-tight text-foreground">
            <span>{{ status?.enabled_bridge_count ?? t('common.dash') }}</span>
            <span class="text-base font-normal text-muted-foreground"> / {{ status?.total_bridge_count ?? t('common.dash') }}</span>
          </p>
          <Badge variant="info" class="mt-2">
            <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
            {{ t('dashboard.activatedSync') }}
          </Badge>
        </CardContent>
      </Card>

      <!-- 凭据卡 -->
      <Card>
        <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-2">
          <CardDescription class="uppercase tracking-wider">{{ t('dashboard.credsLabel') }}</CardDescription>
          <span
            class="flex h-8 w-8 items-center justify-center rounded-lg"
            :class="status?.creds_complete ? 'bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400' : 'bg-amber-50 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400'"
            aria-hidden="true"
          >
            <Shield class="h-4 w-4" />
          </span>
        </CardHeader>
        <CardContent class="pt-2">
          <Skeleton v-if="loading && !status" class="h-9 w-24" />
          <p v-else class="text-3xl font-semibold tracking-tight text-foreground">
            {{ t(credsBadge.labelKey) }}
          </p>
          <Badge :variant="credsBadge.variant" class="mt-2">
            <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
            {{ t('dashboard.credsTarget') }}
          </Badge>
        </CardContent>
      </Card>

      <!-- 监听地址卡 -->
      <Card>
        <CardHeader class="flex flex-row items-start justify-between space-y-0 pb-2">
          <CardDescription class="uppercase tracking-wider">{{ t('dashboard.listenAddr') }}</CardDescription>
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-violet-50 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400" aria-hidden="true">
            <Globe class="h-4 w-4" />
          </span>
        </CardHeader>
        <CardContent class="pt-2">
          <Skeleton v-if="loading && !status" class="h-7 w-32" />
          <p v-else class="break-all font-mono text-base font-medium text-foreground">
            {{ status?.listen_addr || t('common.dash') }}
          </p>
          <Badge variant="info" class="mt-2">
            <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
            {{ t('dashboard.webPanel') }}
          </Badge>
        </CardContent>
      </Card>
    </section>

    <!-- 桥接概览表格 -->
    <Card>
      <CardHeader>
        <div class="flex items-center gap-3">
          <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-brand-50 text-brand-600 dark:bg-brand-900/30 dark:text-brand-400" aria-hidden="true">
            <Network class="h-5 w-5" />
          </span>
          <div class="flex-1">
            <CardTitle>{{ t('dashboard.bridgesOverview') }}</CardTitle>
            <CardDescription>{{ t('dashboard.bridgesOverviewSubtitle') }}</CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <!-- 空态：dashed border + 引导链接 -->
        <div
          v-if="!loading && bridges.length === 0"
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

        <!-- 表格 -->
        <Table v-else-if="bridges.length > 0">
          <TableHeader>
            <TableRow>
              <TableHead>{{ t('dashboard.tableName') }}</TableHead>
              <TableHead>{{ t('dashboard.tableProtocol') }}</TableHead>
              <TableHead>{{ t('dashboard.tableXboardNode') }}</TableHead>
              <TableHead>{{ t('dashboard.tableXuiInbound') }}</TableHead>
              <TableHead>{{ t('dashboard.tableState') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="b in bridges" :key="b.name">
              <TableCell class="font-medium text-foreground">{{ b.name }}</TableCell>
              <TableCell>
                <span class="font-mono text-[13px]">{{ b.protocol }}</span>
                <span v-if="b.flow" class="ml-1 text-xs text-muted-foreground">({{ b.flow }})</span>
              </TableCell>
              <TableCell>
                <span class="font-mono text-[13px]">{{ b.xboard_node_id }}</span>
                <span class="ml-1 text-xs text-muted-foreground">({{ b.xboard_node_type }})</span>
              </TableCell>
              <TableCell><span class="font-mono text-[13px]">{{ b.xui_inbound_id }}</span></TableCell>
              <TableCell>
                <Badge :variant="b.enable ? 'success' : 'secondary'">
                  <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
                  {{ b.enable ? t('common.enabled') : t('common.disabled') }}
                </Badge>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  </div>
</template>
