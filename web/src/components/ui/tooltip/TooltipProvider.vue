<script setup lang="ts">
// TooltipProvider——必须包裹整个 tooltip 子树（提供 reka-ui 上下文：
// delayDuration / skipDelayDuration / disableHoverableContent 等全局参数）。
//
// 项目把它放在 App.vue 根级（批次 7），这样任何 view 内的 <Tooltip> 都自动
// 受全局 provider 管。
//
// delayDuration 默认 700ms 略偏慢；shadcn-vue 默认改 0 让悬停瞬时显示——
// 但运维场景"鼠标偶然划过"会带来误触发烦扰，本项目保留 reka 默认 700ms。
import { TooltipProvider, type TooltipProviderProps } from 'reka-ui'

withDefaults(defineProps<TooltipProviderProps>(), {
  delayDuration: 700,
  skipDelayDuration: 300,
})
</script>

<template>
  <TooltipProvider v-bind="$props">
    <slot />
  </TooltipProvider>
</template>
