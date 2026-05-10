<script setup lang="ts">
// SheetClose——透传到 reka-ui DialogClose。
//
// 用法：as-child 让自定义按钮（含图标 + 样式）作为关闭触发器。
import { DialogClose, type DialogCloseProps } from 'reka-ui'

defineProps<DialogCloseProps>()
</script>

<template>
  <DialogClose v-bind="$props">
    <slot />
  </DialogClose>
</template>
