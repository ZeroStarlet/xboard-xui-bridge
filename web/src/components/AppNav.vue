<script setup lang="ts">
// 左侧导航栏（v0.5 视觉重构）。
//
// 设计：
//   - 暗色面板（surface-900 背景 + 青绿 brand 高亮）—— 与白色主内容区形成强对比，
//     让"导航"与"工作区"语义边界一目了然。
//   - 每项导航带内联 SVG 图标（不引外部 icon 包，保持单二进制大小不变）。
//   - 当前选中项有左侧高亮条 + 背景填充——双线索视觉指引。
//   - 顶部 logo 区有渐变文字让品牌名"亮"出来。
//   - 底部"退出登录"独立块，与导航分开，避免误点。
//
// 不实现：折叠 / 桌面响应式收缩——v0.5 还是固定 240px 宽度。
// 移动设备访问 Web 面板的概率极低（运维多在桌面端），后续真有需求再加。
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

// 内联 SVG 字符串：每个导航项的图标。用 currentColor 让 hover/active 态的色变
// 通过父级 text-* 类自动传导。stroke-width=1.75 比默认 1 更显精致厚重。
const navItems = [
  {
    to: '/dashboard',
    label: '仪表盘',
    icon: '<path stroke-linecap="round" stroke-linejoin="round" d="M3 13.5V19a2 2 0 002 2h3v-7H3zm6 7.5h6v-9H9v9zm8 0h3a2 2 0 002-2v-9h-5v11zM3 11.5h18L12 3 3 11.5z" />',
  },
  {
    to: '/bridges',
    label: '桥接管理',
    icon: '<path stroke-linecap="round" stroke-linejoin="round" d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244" />',
  },
  {
    to: '/settings',
    label: '运行参数',
    icon: '<path stroke-linecap="round" stroke-linejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />',
  },
  {
    to: '/account',
    label: '账户',
    icon: '<path stroke-linecap="round" stroke-linejoin="round" d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z" />',
  },
]

const logoutIcon =
  '<path stroke-linecap="round" stroke-linejoin="round" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />'

async function handleLogout() {
  await auth.logout()
  router.push('/login')
}
</script>

<template>
  <aside
    class="sticky top-0 flex h-screen w-60 shrink-0 flex-col bg-surface-900 text-surface-200"
    aria-label="主导航"
  >
    <!-- 品牌区：渐变文字 logo + 用户名 -->
    <div class="flex flex-col gap-1 px-6 pb-5 pt-7 border-b border-white/[0.06]">
      <h1 class="text-base font-semibold tracking-tight">
        <span class="text-gradient-brand">xboard-xui-bridge</span>
      </h1>
      <p class="text-xs text-surface-400">
        <span v-if="auth.user?.username" class="inline-flex items-center gap-1.5">
          <span class="h-1.5 w-1.5 rounded-full bg-brand-500 animate-pulse-soft" aria-hidden="true" />
          {{ auth.user.username }}
        </span>
        <span v-else class="text-surface-500">未登录</span>
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
        <svg
          class="h-5 w-5 shrink-0"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.75"
          aria-hidden="true"
          v-html="item.icon"
        />
        <span class="nav-label">{{ item.label }}</span>
      </RouterLink>
    </nav>

    <!-- 退出登录 -->
    <div class="px-3 pb-5 pt-3 border-t border-white/[0.06]">
      <button class="nav-item w-full group" @click="handleLogout">
        <span class="nav-indicator" aria-hidden="true" />
        <svg
          class="h-5 w-5 shrink-0"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.75"
          aria-hidden="true"
          v-html="logoutIcon"
        />
        <span class="nav-label">退出登录</span>
      </button>
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
    theme('colors.accent.500')
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
