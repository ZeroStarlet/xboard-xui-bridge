<script setup lang="ts">
// 顶层布局（v0.7 视觉重构 — Bento Live Console）。
//
// 两套 layout：
//
//   - layout=auth：登录 / 错误兜底页，全屏 + 多层光斑渐变 + 玻璃登录卡。
//     v0.7 升级：背景从 v0.6 的静态光斑改为 .aurora-bg 流动极光（缓慢
//     漂移 24s 周期），登录页本身视觉就是"高端浮层"——比纯静态背景更有
//     "进入 Live Console"的仪式感。
//
//   - layout=panel：主面板，新版三段式骨架——
//
//       ┌──── LiveStatusBar (sticky top, h-12, glass) ────┐
//       ├─ AppNav ──┬─── main (RouterView, aurora-bg) ────┤
//       │ (rail /   │                                      │
//       │ expanded) │   page content via RouterView         │
//       └───────────┴──────────────────────────────────────┘
//
//     LiveStatusBar 是 Live Console 调性的关键——常驻顶部、玻璃感、
//     实时引擎心跳；AppNav 折叠 / 展开两态由 layout store 控制；主内容
//     区背景层走 .aurora-bg 让缓慢极光透出来，与卡片磁贴形成"舞台 +
//     聚光"的视觉关系。
//
// 全局 Provider 链（与 v0.6 一致）：
//
//   1. TooltipProvider —— 让所有 view 内的 Tooltip 共享 delayDuration
//   2. CommandPalette  —— 永久挂载在 panel layout 内，由 layout.cmdkOpen
//      控制浮层显隐；自身 onMounted 注册全局 ⌘K / Ctrl+K 监听器
//   3. Toaster         —— 全屏 toast 视口
//
// 深色模式：由 stores/theme.ts 统一管理（与 v0.6 一致），本组件无需感知，
// 所有语义 token + .dark .aurora-bg 自动切换。
import { computed } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppNav from './components/AppNav.vue'
import LiveStatusBar from './components/LiveStatusBar.vue'
import CommandPalette from './components/CommandPalette.vue'
import { TooltipProvider } from './components/ui/tooltip'
import { Toaster } from './components/ui/toast'

const route = useRoute()
const isAuthLayout = computed(() => route.meta.layout === 'auth')
</script>

<template>
  <!--
    TooltipProvider 必须包整个应用——让任意子树里的 <Tooltip> 都能正常工作。
    delayDuration / skipDelayDuration 走 Provider 默认值（700/300ms），
    需要全局调整时只改这一处。
  -->
  <TooltipProvider>
    <!--
      layout=auth：登录页布局
      v0.7 升级：背景换为 .aurora-bg 流动极光（取代 v0.6 的静态光斑组合）。
      登录页主视觉就是"中央浮起的玻璃卡 + 缓慢呼吸的极光"——进入感更强。
      原 v0.6 的多层 absolute 光斑已并入 .aurora-bg 的 radial-gradient
      内部，DOM 更简洁。
    -->
    <div
      v-if="isAuthLayout"
      class="aurora-bg relative flex min-h-screen items-center justify-center overflow-hidden px-4 py-10"
    >
      <div class="relative z-10 w-full max-w-md animate-fade-in-up">
        <RouterView />
      </div>
    </div>

    <!--
      layout=panel：主面板布局
      flex-col 上下排：顶部 LiveStatusBar，下方水平分栏（侧栏 + 主内容）。
      最外层 .aurora-bg 让极光铺满整个面板视野，玻璃 status-bar + 卡片
      磁贴透过来视觉一体。
    -->
    <div v-else class="panel-shell aurora-bg flex min-h-screen flex-col">
      <LiveStatusBar />
      <div class="flex flex-1">
        <AppNav />
        <!--
          main 占满剩余宽度，内容区限制最大宽度让超宽屏不会让 Bento
          网格无意义拉伸——max-w-7xl (~80rem = 1280px) 与 v0.6 一致。
          py-8 + responsive lg:py-10 让上下留白足够。
        -->
        <main class="flex-1">
          <div class="mx-auto max-w-7xl px-6 py-8 lg:px-10 lg:py-10">
            <RouterView v-slot="{ Component }">
              <transition
                enter-active-class="transition-all duration-300 ease-out"
                enter-from-class="opacity-0 translate-y-2"
                enter-to-class="opacity-100 translate-y-0"
                mode="out-in"
              >
                <component :is="Component" />
              </transition>
            </RouterView>
          </div>
        </main>
      </div>

      <!--
        CommandPalette 永久挂载——内部 v-if=cmdkOpen 控制浮层 DOM；
        组件 onMounted 注册全局 ⌘K 监听器，只要 panel layout 在视图栈
        内，监听器就持续生效。auth layout（登录页）不挂载本组件，避免
        未登录用户按 ⌘K 看到导航选项造成混淆——登录页本就只有登录这
        一个动作。
      -->
      <CommandPalette />
    </div>

    <!--
      Toaster 全局 toast 视口——固定在 body 角落（默认右下角，移动端顶部
      中央，详见 ToastViewport.vue）。z-[100] 高于 Sheet/Dialog 的 z-50，
      确保即使有模态打开 toast 仍可见；也高于 cmdk-overlay 的 z-50。
    -->
    <Toaster />
  </TooltipProvider>
</template>
