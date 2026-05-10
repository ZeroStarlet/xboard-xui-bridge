<script setup lang="ts">
// 左侧导航栏（v0.8 视觉重构补丁 — 玻璃面板 + 分组 + LiveDot 状态）。
//
// v0.7 → v0.8 关键差异：
//
//   1. 配色彻底走语义 token——不再硬编码 `bg-surface-900 text-surface-200`
//      暗底色块。新底用 `bg-card/40 + backdrop-blur` 玻璃感，让 .aurora-bg
//      流动极光透过来；亮 / 深模式自动跟随 token，不再视觉割裂。
//
//   2. 信息架构升级：
//      - 中部"导航"分组标题（仅 expanded 显示）
//      - 底部 footer：LiveDot 引擎状态 + ⌘K 提示行（与 status-bar 协作
//        而非冗余——status-bar 是"工具入口集合"，侧栏 footer 是"长在
//        视野余光里的状态指示器"）
//
//   3. nav-item 配色重做：
//      - 默认：text-muted-foreground
//      - hover：bg-secondary + text-foreground（语义 token，深色自动适配）
//      - active：text-primary + 浅品牌底 + 渐变左竖条
//
// 与 LiveStatusBar 的分工：
//
//   - LiveStatusBar：跨页面常驻的"工具栏"——logo / 引擎心跳点（精简版）/
//     桥接计数 / ⌘K / 主题语言 / 用户菜单。
//   - AppNav：纯路由导航 + 底部"状态余光"——同一份 status 在侧栏底部
//     再次呈现，让运维滚动主内容到底部仍能看到"引擎在跑"，比顶部状态条
//     更具陪伴感。
//
// rail / expanded 两态由 layout store 控制——状态条折叠按钮唯一切换源。
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { LayoutDashboard, Network, SlidersHorizontal, UserRound } from 'lucide-vue-next'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import LiveDot from './LiveDot.vue'
import { useLayoutStore } from '@/stores/layout'
import { useStatus } from '@/composables/useStatus'

const { t } = useI18n()
const layout = useLayoutStore()
const { status, lastError: statusError } = useStatus()

// 导航条目——与 router/index.ts 保持同步。
const navItems = [
  { to: '/dashboard', labelKey: 'nav.dashboard' as const, icon: LayoutDashboard },
  { to: '/bridges',   labelKey: 'nav.bridges'   as const, icon: Network },
  { to: '/settings',  labelKey: 'nav.settings'  as const, icon: SlidersHorizontal },
  { to: '/account',   labelKey: 'nav.account'   as const, icon: UserRound },
]

// 引擎心跳点——与 LiveStatusBar 同源派生：
//   - lastError && !status → warn（区分网络故障与已停止）
//   - running               → on
//   - 其他                  → off
const engineDot = computed<'on' | 'warn' | 'off'>(() => {
  if (statusError.value && !status.value) return 'warn'
  if (!status.value) return 'off'
  return status.value.running ? 'on' : 'off'
})

const engineLabel = computed<string>(() => {
  if (statusError.value && !status.value) return t('statusBar.engineUnknown')
  if (!status.value) return t('common.loading')
  return status.value.running ? t('common.running') : t('common.stopped')
})
</script>

<template>
  <!--
    aside 是玻璃面板 + 右边框：
      - bg-card/40 + backdrop-blur-md：让 .aurora-bg 流动极光透过来，
        与 LiveStatusBar 视觉一体化（两者都是 glass 浮层，主面板背景
        统一是 aurora）。
      - border-r：右侧硬切区分"导航 vs 工作区"语义边界。
      - sticky top-12 + h-[calc(100vh-3rem)]：紧贴 LiveStatusBar 下方
        粘附，撑满剩余视口。
      - nav-rail-transition（style.css）：rail/expanded 切换 200ms 过渡。
  -->
  <aside
    class="sticky top-12 flex h-[calc(100vh-3rem)] shrink-0 flex-col border-r bg-card/40 backdrop-blur-md text-foreground nav-rail-transition"
    :class="layout.navRail ? 'w-16' : 'w-60'"
    :aria-label="t('nav.ariaMain')"
  >
    <!--
      顶部分组标题（仅 expanded 显示）—— rail 态下整个顶部隐藏，让导航
      项直接从顶部开始（更紧凑）。
    -->
    <div
      v-if="!layout.navRail"
      class="px-5 pb-2 pt-5 text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground"
    >
      {{ t('nav.groupMain') }}
    </div>
    <div v-else class="h-3 shrink-0" aria-hidden="true" />

    <!-- 导航项区 -->
    <nav class="flex-1 overflow-y-auto px-2 pb-3 space-y-0.5">
      <template v-for="item in navItems" :key="item.to">
        <!-- rail 态：用 Tooltip 包裹，hover 弹完整 label。 -->
        <Tooltip v-if="layout.navRail" :delay-duration="300">
          <TooltipTrigger as-child>
            <RouterLink
              :to="item.to"
              class="nav-item nav-item-rail group"
              active-class="nav-item-active"
              :aria-label="t(item.labelKey)"
            >
              <span class="nav-indicator" aria-hidden="true" />
              <component :is="item.icon" class="h-5 w-5 shrink-0" aria-hidden="true" />
            </RouterLink>
          </TooltipTrigger>
          <TooltipContent side="right" :side-offset="12">
            {{ t(item.labelKey) }}
          </TooltipContent>
        </Tooltip>

        <!-- expanded 态：图标 + 文字 -->
        <RouterLink
          v-else
          :to="item.to"
          class="nav-item group"
          active-class="nav-item-active"
        >
          <span class="nav-indicator" aria-hidden="true" />
          <component :is="item.icon" class="h-5 w-5 shrink-0" aria-hidden="true" />
          <span class="nav-label">{{ t(item.labelKey) }}</span>
        </RouterLink>
      </template>
    </nav>

    <!--
      底部 footer：LiveDot 引擎状态——状态条 status-bar 已承担"工具入口
      集合"职责，本 footer 只做"长在视野余光里的状态指示器"，让滚动主
      内容到中段也能在侧栏底部看到引擎心跳。
      ⌘K 入口与 LiveStatusBar 重复，本 footer 不再展示（v0.8 第 2 轮
      Codex nit #2 修复）。
    -->
    <div class="border-t bg-card/20 px-3 py-3">
      <!-- rail 态：用 Tooltip 包裹 LiveDot 与导航项 rail Tooltip 模式
           保持一致——hover 弹完整状态文字，比 title 属性更可控 + 与
           rail 导航交互语言统一（v0.8 第 2 轮 Codex nit #1 修复）。 -->
      <Tooltip v-if="layout.navRail" :delay-duration="300">
        <TooltipTrigger as-child>
          <div
            class="flex cursor-default items-center justify-center py-1"
            tabindex="-1"
            :aria-label="engineLabel"
          >
            <LiveDot :status="engineDot" size="md" />
          </div>
        </TooltipTrigger>
        <TooltipContent side="right" :side-offset="12">
          {{ t('nav.engineLabel') }} · {{ engineLabel }}
        </TooltipContent>
      </Tooltip>
      <div
        v-else
        class="flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-xs"
      >
        <LiveDot :status="engineDot" size="sm" />
        <span class="text-muted-foreground">{{ t('nav.engineLabel') }}</span>
        <span class="ml-auto font-medium text-foreground">{{ engineLabel }}</span>
      </div>
    </div>
  </aside>
</template>

<style scoped>
/*
 * v0.8 nav-item 配色重做——彻底走 shadcn 语义 token，亮 / 深模式自动适配。
 *
 * 与 v0.7 暗底 surface-900 配色不同：
 *   - 默认 text-muted-foreground 而非 surface-300（亮模式 slate-500、深
 *     模式 slate-400 自动切换）
 *   - hover bg-secondary（亮 slate-100、深 surface-900）+ text-foreground
 *   - active 用 brand 浅底 + 文字 brand-700（深 brand-300），左竖条沿用
 *     渐变 brand→info——视觉连续性保留
 */
.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.75rem;
  border-radius: 0.625rem;
  color: hsl(var(--muted-foreground));
  font-size: 0.875rem;
  font-weight: 500;
  transition: all 150ms ease-out;
  cursor: pointer;
}
.nav-item-rail {
  padding: 0.5rem 0;
  justify-content: center;
}
.nav-item:hover {
  background: hsl(var(--secondary));
  color: hsl(var(--foreground));
}
.nav-item:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px hsl(var(--ring));
}
.nav-item-active {
  background: linear-gradient(
    135deg,
    rgba(16, 185, 129, 0.10),
    rgba(59, 130, 246, 0.06)
  );
  color: theme('colors.brand.700');
}
:global(.dark) .nav-item-active {
  background: linear-gradient(
    135deg,
    rgba(52, 211, 153, 0.14),
    rgba(96, 165, 250, 0.08)
  );
  color: theme('colors.brand.300');
}
.nav-indicator {
  position: absolute;
  left: -0.5rem;
  top: 50%;
  transform: translateY(-50%) scaleY(0);
  width: 3px;
  height: 1.25rem;
  border-radius: 9999px;
  background: linear-gradient(
    180deg,
    theme('colors.brand.500'),
    theme('colors.info.500')
  );
  transition: transform 200ms cubic-bezier(0.16, 1, 0.3, 1);
}
.nav-item-active .nav-indicator {
  transform: translateY(-50%) scaleY(1);
}
.nav-label {
  flex: 1;
  text-align: left;
}
</style>
