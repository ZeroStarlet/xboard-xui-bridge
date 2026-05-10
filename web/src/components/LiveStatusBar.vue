<script setup lang="ts">
// 顶部 Live Status Bar（v0.7 视觉重构 — Bento Live Console）。
//
// 信息架构（左→中→右）：
//
//   [折叠按钮] [logo] [引擎心跳]   [桥接计数] [凭据点]   [⌘K] [主题] [语言] [用户菜单]
//
// 三段式：
//
//   - 左段（始终可见）：折叠侧栏按钮 + 品牌 logo + 引擎心跳点。
//     折叠按钮用 PanelLeft / PanelLeftOpen 双图标随 navRail 状态切换；
//     引擎心跳点：running=on(emerald) / stopped=off(slate) / 加载失败=warn(amber)。
//
//   - 中段（lg 以上可见）：关键运行计数。"X/Y 桥接" + 凭据是否完整。
//     窄屏（< lg）下隐藏让出空间给右段——关键数据在 Dashboard 第一屏
//     已有大字号展示，状态条上的数字属于"瞥一眼就知道"型快查。
//
//   - 右段（始终可见）：⌘K 命令面板入口（带 kbd 提示） + ThemeToggle +
//     LanguageSwitcher + 用户菜单（账户跳转 / 退出登录）。
//
// 跨组件协作：
//
//   - useStatus() 共享 status 数据：与 Dashboard 同源，避免双倍请求。
//   - useLayoutStore() 控制侧栏折叠 + 命令面板开关：折叠按钮调
//     toggleNavRail()，⌘K 按钮调 openCmdK()——CommandPalette 自身监听
//     cmdkOpen 显示 / 隐藏。
//
// i18n：所有文案走 t()，新 statusBar.* / common.* 键值已加入 zh-CN /
// en-US locale。
//
// 可访问性：
//
//   - <header role="banner"> 让屏幕阅读器把状态条识别为"页面横幅"区。
//   - 折叠按钮、⌘K 按钮、用户菜单触发器均带 aria-label。
//   - 引擎心跳点 LiveDot 自身 aria-hidden=true，由相邻文字承担语义。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import {
  PanelLeftClose,
  PanelLeftOpen,
  Zap,
  Search,
  Network,
  Shield,
  ChevronDown,
  UserRound,
  LogOut,
} from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import LiveDot from './LiveDot.vue'
import ThemeToggle from './ThemeToggle.vue'
import LanguageSwitcher from './LanguageSwitcher.vue'
import { useAuthStore } from '@/stores/auth'
import { useLayoutStore } from '@/stores/layout'
import { useToast } from '@/composables/useToast'
import { useStatus } from '@/composables/useStatus'

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const layout = useLayoutStore()
const { toast } = useToast()
const { status, lastError } = useStatus()

// 引擎心跳点状态派生：
//
//   - status 未加载 + 无错误 → off（首屏未拉到数据时显示离线状态）
//   - status 加载失败       → warn（lastError 非空，显示琥珀色提示）
//   - running=true          → on（emerald）
//   - running=false         → off（slate）
//
// 不在 store 加载失败时返回 'off'：那会让"网络故障"与"引擎已停止"两种
// 截然不同的状态视觉等同，运维误判风险高。warn 色独立明示"中间件本身
// 拉数据失败"。
const engineDotStatus = computed<'on' | 'warn' | 'off'>(() => {
  if (lastError.value && !status.value) return 'warn'
  if (!status.value) return 'off'
  return status.value.running ? 'on' : 'off'
})

const engineLabel = computed<string>(() => {
  if (lastError.value && !status.value) return t('statusBar.engineUnknown')
  if (!status.value) return t('common.loading')
  return status.value.running ? t('common.running') : t('common.stopped')
})

// 凭据完整性派生：用 LiveDot 三态——
//   creds_complete=true → on
//   creds_complete=false → warn
//   未加载 → off
const credsDotStatus = computed<'on' | 'warn' | 'off'>(() => {
  if (!status.value) return 'off'
  return status.value.creds_complete ? 'on' : 'warn'
})

async function handleLogout(): Promise<void> {
  await auth.logout()
  toast({
    title: t('common.logout'),
    variant: 'default',
    duration: 2000,
  })
  router.push('/login')
}

function goAccount(): void {
  router.push('/account')
}
</script>

<template>
  <header class="status-bar" role="banner" :aria-label="t('statusBar.aria')">
    <!-- ============================================================
         左段：侧栏折叠按钮 + 品牌 logo + 引擎心跳
         ============================================================ -->

    <!-- 折叠按钮：用 reka-ui Tooltip 给悬停提示？这里直接 aria-label
         即可——标题栏视觉简洁优先，悬停提示与 status 文字混排会嘈杂。 -->
    <Button
      variant="ghost"
      size="icon"
      class="shrink-0"
      :aria-label="t('statusBar.toggleNav')"
      @click="layout.toggleNavRail"
    >
      <PanelLeftClose v-if="!layout.navRail" class="size-[1.2rem]" />
      <PanelLeftOpen v-else class="size-[1.2rem]" />
    </Button>

    <!-- 品牌 logo + 文字：点击回 Dashboard。窄屏（< sm）只显示图标。 -->
    <RouterLink
      to="/dashboard"
      class="flex shrink-0 items-center gap-2 rounded-lg px-1.5 py-1 transition-colors hover:bg-secondary/60"
      :aria-label="t('statusBar.homeAria')"
    >
      <span
        class="flex h-7 w-7 items-center justify-center rounded-lg shadow-soft"
        style="background: linear-gradient(135deg, #10b981, #3b82f6);"
        aria-hidden="true"
      >
        <Zap class="h-4 w-4 text-white" stroke-width="2.5" />
      </span>
      <span class="hidden text-sm font-semibold tracking-tight sm:inline">
        <span class="text-gradient-brand">xboard-xui-bridge</span>
      </span>
    </RouterLink>

    <!-- 引擎心跳点 + 文字（始终可见，状态条核心信号） -->
    <div class="hidden items-center gap-1.5 rounded-full border bg-card/60 px-2.5 py-1 text-xs sm:flex">
      <LiveDot :status="engineDotStatus" size="sm" />
      <span class="font-medium text-foreground">{{ engineLabel }}</span>
    </div>

    <!-- ============================================================
         中段（lg 以上）：桥接计数 + 凭据状态点
         ============================================================ -->
    <div class="ml-2 hidden items-center gap-3 lg:flex">
      <Separator orientation="vertical" class="h-5" />

      <div class="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Network class="size-3.5" aria-hidden="true" />
        <span>{{ t('statusBar.bridgesActive') }}</span>
        <span class="display-num text-foreground">
          {{ status?.enabled_bridge_count ?? '—' }}
          <span class="text-muted-foreground">/</span>
          {{ status?.total_bridge_count ?? '—' }}
        </span>
      </div>

      <Separator orientation="vertical" class="h-5" />

      <div class="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Shield class="size-3.5" aria-hidden="true" />
        <span>{{ t('statusBar.credsLabel') }}</span>
        <LiveDot :status="credsDotStatus" size="sm" />
        <span class="font-medium text-foreground">
          {{ status?.creds_complete ? t('common.configured') : t('common.incomplete') }}
        </span>
      </div>
    </div>

    <!-- ============================================================
         右段：⌘K 入口 + 主题 / 语言切换 + 用户菜单
         ============================================================ -->
    <div class="ml-auto flex shrink-0 items-center gap-1">
      <!-- ⌘K 命令面板入口：仿 Linear / Vercel 风格——长条型按钮带
           Search 图标 + 占位文字 + ⌘K kbd 提示。窄屏退化为纯图标按钮。 -->
      <button
        type="button"
        class="hidden h-8 items-center gap-2 rounded-lg border bg-card/60 px-2.5 text-xs text-muted-foreground transition-colors hover:bg-secondary md:inline-flex"
        :aria-label="t('statusBar.cmdkAria')"
        @click="layout.openCmdK"
      >
        <Search class="size-3.5" aria-hidden="true" />
        <span>{{ t('statusBar.cmdkPlaceholder') }}</span>
        <kbd class="cmdk-shortcut">⌘K</kbd>
      </button>
      <Button
        variant="ghost"
        size="icon"
        class="md:hidden"
        :aria-label="t('statusBar.cmdkAria')"
        @click="layout.openCmdK"
      >
        <Search class="size-[1.2rem]" />
      </Button>

      <ThemeToggle />
      <LanguageSwitcher />

      <Separator orientation="vertical" class="mx-1 h-5" />

      <!-- 用户菜单：触发器显示用户名 + ChevronDown；菜单包账户 / 退出。
           窄屏下隐藏用户名，仅显示头像图标节省空间。 -->
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <Button
            variant="ghost"
            size="sm"
            class="h-8 gap-1.5 px-2"
            :aria-label="t('statusBar.userMenuAria')"
          >
            <span
              class="flex h-6 w-6 items-center justify-center rounded-full bg-brand-50 text-[11px] font-semibold text-brand-700 dark:bg-brand-900/40 dark:text-brand-300"
              aria-hidden="true"
            >
              {{ (auth.user?.username || '?').slice(0, 1).toUpperCase() }}
            </span>
            <span class="hidden text-sm font-medium sm:inline">
              {{ auth.user?.username || t('nav.notLoggedIn') }}
            </span>
            <ChevronDown class="size-3.5 text-muted-foreground" aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" class="w-44">
          <DropdownMenuItem @select="goAccount">
            <UserRound class="size-4" aria-hidden="true" />
            <span class="flex-1">{{ t('nav.account') }}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem @select="handleLogout">
            <LogOut class="size-4" aria-hidden="true" />
            <span class="flex-1">{{ t('common.logout') }}</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  </header>
</template>
