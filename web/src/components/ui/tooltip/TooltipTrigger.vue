<script setup lang="ts">
// TooltipTrigger——透传 reka-ui TooltipTrigger。as-child 让自定义按钮 / 图标
// 作为触发器。
import { TooltipTrigger, type TooltipTriggerProps } from 'reka-ui'

defineProps<TooltipTriggerProps>()
</script>

<template>
  <TooltipTrigger v-bind="$props">
    <slot />
  </TooltipTrigger>
</template>
