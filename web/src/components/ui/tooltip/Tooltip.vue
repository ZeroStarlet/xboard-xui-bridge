<script setup lang="ts">
// Tooltip 根——透传 reka-ui TooltipRoot。
//
// **不内嵌 TooltipProvider**：必须在 App.vue 根级挂一次 <TooltipProvider />，
// 让全局 delayDuration / skipDelayDuration 配置统一。
//
// 历史 v0.6 初版在本组件内嵌 Provider 让单 Tooltip"自包含可用"，但代价是
// 屏蔽 App.vue 全局 Provider 的配置——例如全局想把 delayDuration 改成 0
// 让 tooltip 立即响应，会被本层 Provider 重写为默认 700。批次 6 Codex 第 1
// 轮指出此设计风险后改为"依赖外层 Provider"——与 shadcn-vue 官方约定一致。
//
// 用法约束：消费者必须保证 Tooltip 被 TooltipProvider 包裹，否则 reka 会抛
// "Tooltip must be used within TooltipProvider" 错误。App.vue 已经在根级
// 装配（批次 7），所有 view 内的 Tooltip 都自动受全局 provider 管。
import { TooltipRoot, type TooltipRootEmits, type TooltipRootProps, useForwardPropsEmits } from 'reka-ui'

const props = defineProps<TooltipRootProps>()
const emits = defineEmits<TooltipRootEmits>()

const forwarded = useForwardPropsEmits(props, emits)
</script>

<template>
  <TooltipRoot v-bind="forwarded">
    <slot />
  </TooltipRoot>
</template>
