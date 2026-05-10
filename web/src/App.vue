<script setup lang="ts">
// 顶层布局（v0.6 视觉重构 — Fluent Design + dark mode + i18n）。
//
// 两套 layout：
//   - layout=auth：登录 / 错误兜底页，全屏 + Mica 渐变背景，无导航栏。
//   - layout=panel：主面板，固定左侧导航 + 主内容区滚动。
//
// 全局 Provider 链：
//   1. TooltipProvider —— 让所有 view 内的 Tooltip 共享 delayDuration 配置
//      （reka-ui 要求每个 Tooltip 必须在 Provider 内）
//   2. Toaster —— 全屏 toast 视口，业务代码通过 useToast() 触发的所有
//      非阻塞通知都渲染在这里
//
// 深色模式：由 stores/theme.ts 在 main.ts 装配时给 <html> 加 .dark class，
// 本组件无需感知，所有语义 token（bg-background / text-foreground 等）自动
// 切换，shadcn-vue 组件源码也无需任何改动。
//
// 渐变背景：登录页用 Mica 风的"青绿 → 紫蓝"对角渐变 + 散景光斑（v0.5 视觉
// 资产保留，运维已有视觉记忆，不必重做）；主面板用 .mica-bg 极淡渐变叠加
// 让大面板有"层次感"——不抢内容焦点。
import { computed } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppNav from './components/AppNav.vue'
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
      渐变背景 + 双散景光斑——"高端" 视觉的关键技巧：单色平铺会显得寡淡，
      多重叠加的弥散光斑能让视线有层次感。光斑用 absolute 元素 + 模糊
      渐变实现，无需图片，零额外字节。深色模式下光斑亮度提高，避免被
      surface-950 极深背景吞噬。
    -->
    <div
      v-if="isAuthLayout"
      class="auth-shell relative min-h-screen flex items-center justify-center overflow-hidden px-4 py-10"
    >
      <!-- 装饰性光斑：低饱和、大尺寸、强模糊，让背景有"呼吸感"。 -->
      <div
        class="pointer-events-none absolute -top-40 -left-40 h-[480px] w-[480px] rounded-full opacity-50 blur-3xl dark:opacity-30"
        style="background: radial-gradient(closest-side, rgba(16, 185, 129, 0.55), transparent 70%);"
        aria-hidden="true"
      />
      <div
        class="pointer-events-none absolute -bottom-40 -right-40 h-[520px] w-[520px] rounded-full opacity-50 blur-3xl dark:opacity-30"
        style="background: radial-gradient(closest-side, rgba(59, 130, 246, 0.55), transparent 70%);"
        aria-hidden="true"
      />
      <div
        class="pointer-events-none absolute top-1/3 left-1/3 h-[300px] w-[300px] rounded-full opacity-30 blur-3xl dark:opacity-20"
        style="background: radial-gradient(closest-side, rgba(168, 85, 247, 0.45), transparent 70%);"
        aria-hidden="true"
      />
      <div class="relative z-10 w-full max-w-md animate-fade-in-up">
        <RouterView />
      </div>
    </div>

    <!--
      layout=panel：主面板布局
      固定宽度左导航 + 弹性主内容；主内容区用 .mica-bg（淡渐变叠加）让
      大面板有 Fluent 风的层次感而不显寡淡。
    -->
    <div v-else class="panel-shell flex min-h-screen mica-bg">
      <AppNav />
      <main class="flex-1 overflow-y-auto">
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
      Toaster 全局 toast 视口——固定在 body 角落（默认右下角，移动端顶部
      中央，详见 ToastViewport.vue）。任何 view 调 useToast().toast({...})
      都会渲染到这里。z-[100] 高于 Sheet/Dialog 的 z-50，确保即使有模态打开
      toast 仍可见。
    -->
    <Toaster />
  </TooltipProvider>
</template>

<style scoped>
/*
 * auth-shell 渐变背景：青绿 → 灰蓝 → 紫色，保持低饱和让光斑成为视觉主角。
 * 深色模式下背景换成 surface-900 → surface-950，光斑 opacity 已在 template
 * 内的 dark: 修饰符调低，整体观感保持"深色 + 微光"。
 */
.auth-shell {
  background: linear-gradient(135deg, #f0fdfa 0%, #f8fafc 50%, #faf5ff 100%);
}
:global(.dark) .auth-shell {
  background: linear-gradient(135deg, #020617 0%, #0f172a 50%, #1e1b4b 100%);
}
</style>
