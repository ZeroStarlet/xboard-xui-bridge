<script setup lang="ts">
// 左侧导航栏（v0.6 视觉重构 — i18n + 深色 + ThemeToggle / LanguageSwitcher）。
//
// 设计：
//   - 暗色面板（surface-900 背景 + 青绿 brand 高亮）—— 与白色主内容区形成强对比，
//     让"导航"与"工作区"语义边界一目了然。在 .dark 主模式下，主内容区也是
//     深色，nav 仍保持 surface-900，但比主背景 surface-950 略亮一档（约 +3%
//     亮度），层次依然分明。
//   - 每项导航带 lucide-vue-next 图标（替代 v0.5 的内联 SVG，让源码更干净）。
//   - 当前选中项有左侧高亮条 + 渐变填充——双线索视觉指引，与 v0.5 一致。
//   - 顶部 logo 区有渐变文字让品牌名"亮"出来。
//   - 底部三段式：ThemeToggle + LanguageSwitcher 在第一行（icon button 并排）；
//     退出登录占第二行（全宽 nav-style 按钮）。
//
// i18n：所有文案通过 t() 取，包括 nav 项 label / aria-label / 退出登录文字。
//
// 不实现：折叠 / 桌面响应式收缩——v0.6 仍是固定 240px 宽度。运维多在桌面端，
// 移动设备访问 Web 面板的概率极低，后续真有需求再加。
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { LayoutDashboard, Network, SlidersHorizontal, UserRound, LogOut } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import ThemeToggle from './ThemeToggle.vue'
import LanguageSwitcher from './LanguageSwitcher.vue'

const { t } = useI18n()
const router = useRouter()
const auth = useAuthStore()
const { toast } = useToast()

// 导航条目——与 router/index.ts 保持同步。每项的 labelKey 指向 i18n
// messages 的 nav.* 节点，与 zh-CN.json / en-US.json 对齐。
const navItems = [
  { to: '/dashboard', labelKey: 'nav.dashboard' as const, icon: LayoutDashboard },
  { to: '/bridges',   labelKey: 'nav.bridges'   as const, icon: Network },
  { to: '/settings',  labelKey: 'nav.settings'  as const, icon: SlidersHorizontal },
  { to: '/account',   labelKey: 'nav.account'   as const, icon: UserRound },
]

async function handleLogout() {
  await auth.logout()
  // 跳转登录页前给个 toast 反馈——避免用户突然看到登录页不知所措。
  // 只放 title 不放 description：单一动作通知（"退出登录"）描述会重复 title
  // 文字，视觉与屏幕阅读器都冗余；将来若要加"为安全起见已清空所有会话"
  // 类长描述，再补 i18n key 与 description 字段。
  toast({
    title: t('common.logout'),
    variant: 'default',
    duration: 2000,
  })
  router.push('/login')
}
</script>

<template>
  <aside
    class="sticky top-0 flex h-screen w-60 shrink-0 flex-col bg-surface-900 text-surface-200"
    :aria-label="t('nav.ariaMain')"
  >
    <!-- 品牌区：渐变文字 logo + 用户名状态 -->
    <div class="flex flex-col gap-1 px-6 pb-5 pt-7 border-b border-white/[0.06]">
      <h1 class="text-base font-semibold tracking-tight">
        <span class="text-gradient-brand">xboard-xui-bridge</span>
      </h1>
      <p class="text-xs text-surface-400">
        <span v-if="auth.user?.username" class="inline-flex items-center gap-1.5">
          <!-- 在线状态点：脉冲动画提示"系统在线" -->
          <span class="h-1.5 w-1.5 rounded-full bg-brand-500 animate-pulse-soft" aria-hidden="true" />
          {{ auth.user.username }}
        </span>
        <span v-else class="text-surface-500">{{ t('nav.notLoggedIn') }}</span>
      </p>
    </div>

    <!-- 导航项 -->
    <nav class="flex-1 px-3 py-5 space-y-0.5">
      <RouterLink
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="nav-item group"
        active-class="nav-item-active"
      >
        <!-- 选中态左侧高亮条——通过 active 类的伪元素或单独 span 渲染。
             这里用单独 span 让动效（scale）更细腻。 -->
        <span class="nav-indicator" aria-hidden="true" />
        <!-- aria-hidden="true"：图标是装饰性，文字标签已表达语义；
             否则屏幕阅读器会朗读"image image image"等噪声。 -->
        <component :is="item.icon" class="h-5 w-5 shrink-0" aria-hidden="true" />
        <span class="nav-label">{{ t(item.labelKey) }}</span>
      </RouterLink>
    </nav>

    <!-- 底部 utility 区：主题/语言切换 + 退出登录 -->
    <div class="border-t border-white/[0.06]">
      <!--
        主题/语言切换器横排：图标按钮，hover 才高亮。
        flex + gap-1 让两个 ghost icon button 之间有 4px 间距；不嵌额外 div
        让 flex 直接作用于按钮（v0.6 初版用嵌套 div 但 gap 在外层未生效，
        批次 7 Codex 第 1 轮指出，已合并到本节点）。
        .theme-utility-area 类用于 scoped style 内的 :deep(button) 颜色覆写
        （让 ghost button 在暗色 nav 面板上可读）。
      -->
      <div class="theme-utility-area flex items-center justify-center gap-1 px-3 py-3">
        <ThemeToggle />
        <LanguageSwitcher />
      </div>
      <!-- 退出登录单行 -->
      <div class="px-3 pb-5">
        <button class="nav-item w-full group" @click="handleLogout">
          <span class="nav-indicator" aria-hidden="true" />
          <LogOut class="h-5 w-5 shrink-0" aria-hidden="true" />
          <span class="nav-label">{{ t('common.logout') }}</span>
        </button>
      </div>
    </div>
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
.nav-item:hover {
  background: rgba(255, 255, 255, 0.04);
  color: white;
}
/* 键盘焦点态——全局 style.css 移除了 button:focus-visible 的默认 outline，
 * 这里用 brand 色 box-shadow 环补回，让 Tab 键导航时焦点位置一目了然。
 * 用 box-shadow 而非 outline：避免与 nav-indicator 的左侧高亮条
 * absolute 定位重叠造成视觉错乱（outline 会绕到指示条外侧）。
 */
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
  left: -0.75rem;
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

/*
 * theme-utility-area：让暗色 nav 面板上的 ghost button 可读。
 * 默认 button-ghost 是 text-foreground hover:bg-accent；在 surface-900
 * 暗色面板上 foreground 仍能勉强看到（亮模式下 foreground=slate-900 太暗
 * 反而看不见，深色模式下 foreground=slate-100 正常）。这里全局覆写：
 *   - 文字色固定为 surface-300（暗模式下亮主色调），与 nav-item 默认色一致
 *   - hover 用 white/[0.08] 与 nav-item:hover 视觉一致
 * 用 :deep 穿透 ghost button 的 cn() 默认类。
 */
.theme-utility-area :deep(button) {
  color: theme('colors.surface.300');
}
.theme-utility-area :deep(button):hover {
  background-color: rgba(255, 255, 255, 0.08);
  color: white;
}
</style>
