<script setup lang="ts">
// 顶层布局（v0.5 视觉重构）。
//
// 两套布局：
//   - layout=auth：登录 / 错误兜底页，全屏 + 渐变背景，无导航栏。
//   - layout=panel：主面板，固定左侧导航 + 主内容区滚动。
//
// 渐变背景：登录页用"青绿 → 紫蓝"对角渐变 + 散景光斑，营造高端感；
// 主面板用极浅的 surface-50 单色——避免内容区被花哨背景干扰。
import { computed } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppNav from './components/AppNav.vue'

const route = useRoute()
const isAuthLayout = computed(() => route.meta.layout === 'auth')
</script>

<template>
  <!--
    layout=auth：登录页布局
    渐变背景 + 双散景光斑——"高端" 视觉的关键技巧：单色平铺会显得寡淡，
    多重叠加的弥散光斑能让视线有层次感。光斑用 ::before / ::after 通过
    模糊渐变实现，无需图片，零额外字节。
  -->
  <div
    v-if="isAuthLayout"
    class="auth-shell relative min-h-screen flex items-center justify-center overflow-hidden px-4 py-10"
  >
    <!-- 装饰性光斑：低饱和、大尺寸、强模糊，让背景有"呼吸感"。 -->
    <div
      class="pointer-events-none absolute -top-40 -left-40 h-[480px] w-[480px] rounded-full opacity-50 blur-3xl"
      style="background: radial-gradient(closest-side, rgba(16, 185, 129, 0.55), transparent 70%);"
      aria-hidden="true"
    />
    <div
      class="pointer-events-none absolute -bottom-40 -right-40 h-[520px] w-[520px] rounded-full opacity-50 blur-3xl"
      style="background: radial-gradient(closest-side, rgba(59, 130, 246, 0.55), transparent 70%);"
      aria-hidden="true"
    />
    <div
      class="pointer-events-none absolute top-1/3 left-1/3 h-[300px] w-[300px] rounded-full opacity-30 blur-3xl"
      style="background: radial-gradient(closest-side, rgba(168, 85, 247, 0.45), transparent 70%);"
      aria-hidden="true"
    />
    <div class="relative z-10 w-full max-w-md animate-fade-in-up">
      <RouterView />
    </div>
  </div>

  <!--
    layout=panel：主面板布局
    固定宽度左导航 + 弹性主内容；主内容内 padding 足够宽让卡片有呼吸空间。
  -->
  <div v-else class="panel-shell flex min-h-screen bg-surface-50">
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
</template>

<style scoped>
/* auth-shell 渐变背景：青绿 → 灰蓝 → 紫色，保持低饱和让光斑成为视觉主角。 */
.auth-shell {
  background: linear-gradient(135deg, #f0fdfa 0%, #f8fafc 50%, #faf5ff 100%);
}
</style>
