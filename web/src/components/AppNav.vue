<script setup lang="ts">
// 左侧导航栏（v0.7 视觉重构 — Bento Live Console）。
//
// 与 v0.6 的差异：
//
//   1. 折叠 / 展开两态：
//      - rail (w-16) 仅图标，hover 弹 Tooltip 提示 label；
//      - expanded (w-60) 图标 + 文字 + 当前选中竖条高亮；
//      由 useLayoutStore() 的 navRail 控制（持久化到 localStorage）。
//
//   2. 顶部 brand / 底部 utility 区移除：v0.6 的 logo / username / 主题
//      切换 / 语言切换 / 退出登录全部迁到顶部 LiveStatusBar——侧栏只
//      保留"路由导航"语义，更纯粹、空间更充裕。
//
//   3. 视觉语言保留 v0.6：surface-900 暗底 + emerald 高亮 + 左竖条
//      指示器，与"工作区主背景"形成强对比。在 .dark 下仍是 surface-900
//      （比主背景 surface-950 略亮一档）。
//
// 可访问性：
//
//   - Tooltip 仅在 rail 态启用：展开时文字已可见，再加 tooltip 反而冗余；
//     用 v-if="navRail" 在模板侧切换 Tooltip 包裹，避免运行时性能损耗。
//   - aria-label 在 rail 态作为唯一可访问文本；展开态由可见 label
//     自动承担。
import { useI18n } from 'vue-i18n'
import { LayoutDashboard, Network, SlidersHorizontal, UserRound } from 'lucide-vue-next'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { useLayoutStore } from '@/stores/layout'

const { t } = useI18n()
const layout = useLayoutStore()

// 导航条目——与 router/index.ts 保持同步。labelKey 指向 i18n
// messages 的 nav.* 节点，与 zh-CN.json / en-US.json 对齐。
const navItems = [
  { to: '/dashboard', labelKey: 'nav.dashboard' as const, icon: LayoutDashboard },
  { to: '/bridges',   labelKey: 'nav.bridges'   as const, icon: Network },
  { to: '/settings',  labelKey: 'nav.settings'  as const, icon: SlidersHorizontal },
  { to: '/account',   labelKey: 'nav.account'   as const, icon: UserRound },
]
</script>

<template>
  <!--
    aside 宽度随 navRail 切换，nav-rail-transition 提供 200ms 过渡动画
    （style.css 注册）。sticky top-12（48px）让侧栏在 LiveStatusBar
    （h-12）正下方保持粘附；高度 calc(100vh - 3rem) 撑满剩余视口高度。
    与 LiveStatusBar 的 sticky 配合形成"T 形固定外壳"——顶部状态条 +
    左侧导航始终可见，主内容区在右下方滚动。
  -->
  <aside
    class="sticky top-12 flex h-[calc(100vh-3rem)] shrink-0 flex-col bg-surface-900 text-surface-200 nav-rail-transition"
    :class="layout.navRail ? 'w-16' : 'w-60'"
    :aria-label="t('nav.ariaMain')"
  >
    <!-- 顶部空白：留 12px 给侧栏顶部微调"呼吸感"（不能完全贴到顶部，
         视觉太局促）。 -->
    <div class="h-3 shrink-0" aria-hidden="true" />

    <!-- 导航项区 -->
    <nav class="flex-1 overflow-y-auto px-2 py-3 space-y-0.5">
      <template v-for="item in navItems" :key="item.to">
        <!-- rail 态：用 Tooltip 包裹，hover 弹完整 label。
             展开态：直接渲染 RouterLink。
             避免 v-if/else 复用模板用 component :is 切换——这里用
             两个独立分支让模板更易读。 -->
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

    <!-- 底部 spacer：给侧栏底部 8px 呼吸感（无 utility 区后视觉避免太顶）。 -->
    <div class="h-2 shrink-0" aria-hidden="true" />
  </aside>
</template>

<style scoped>
.nav-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.625rem 0.875rem;
  border-radius: 0.75rem;
  color: theme('colors.surface.300');
  font-size: 0.875rem;
  font-weight: 500;
  transition: all 150ms ease-out;
  cursor: pointer;
}
/* rail 态：去掉 padding 左右差，让图标在 64px 容器内严格居中。
 * gap 不影响（仅一个图标，无文字）；padding 0.625rem 上下 + 0 左右
 * 让 nav-indicator 的 left:-0.5rem 仍在容器内可见。 */
.nav-item-rail {
  padding: 0.625rem 0;
  justify-content: center;
}
.nav-item:hover {
  background: rgba(255, 255, 255, 0.04);
  color: white;
}
/* 键盘焦点态——与 v0.6 一致，box-shadow 环避免与 nav-indicator 重叠。 */
.nav-item:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px theme('colors.brand.400');
}
.nav-item-active {
  background: linear-gradient(
    135deg,
    rgba(16, 185, 129, 0.18),
    rgba(59, 130, 246, 0.10)
  );
  color: white;
  box-shadow: inset 0 0 0 1px rgba(16, 185, 129, 0.20);
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
    theme('colors.brand.400'),
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
