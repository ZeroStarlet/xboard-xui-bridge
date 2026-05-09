<script setup lang="ts">
// 顶层布局：登录页全屏，主面板带左侧导航。判断逻辑放 router 里更纯净，
// 但顶级组件至少需要识别"是否登录页路径"以决定是否渲染导航条——
// 简化做法是用 route.meta.layout = 'auth' / 'panel' 区分。
import { computed } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppNav from './components/AppNav.vue'

const route = useRoute()
const isAuthLayout = computed(() => route.meta.layout === 'auth')
</script>

<template>
  <div v-if="isAuthLayout" class="min-h-screen flex items-center justify-center bg-gray-100 px-4">
    <RouterView />
  </div>
  <div v-else class="min-h-screen flex">
    <AppNav />
    <main class="flex-1 overflow-y-auto p-8">
      <RouterView />
    </main>
  </div>
</template>
