<script setup lang="ts">
// SheetTrigger——透传到 reka-ui DialogTrigger。
//
// 默认渲染为 <button>；as-child 把 trigger 行为附加到子节点（让 RouterLink
// / Button 组件作为触发器），避免 button-in-button 嵌套。
import { DialogTrigger, type DialogTriggerProps } from 'reka-ui'

defineProps<DialogTriggerProps>()
</script>

<template>
  <DialogTrigger v-bind="$props">
    <slot />
  </DialogTrigger>
</template>
