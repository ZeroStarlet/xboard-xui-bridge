<script setup lang="ts">
// DropdownMenuTrigger——透传 reka-ui DropdownMenuTrigger。
// 默认 as="button"；as-child 让自定义按钮（含图标/样式）作为触发器，
// 避免 button-in-button 嵌套。
import { DropdownMenuTrigger, type DropdownMenuTriggerProps } from 'reka-ui'

defineProps<DropdownMenuTriggerProps>()
</script>

<template>
  <DropdownMenuTrigger v-bind="$props">
    <slot />
  </DropdownMenuTrigger>
</template>
