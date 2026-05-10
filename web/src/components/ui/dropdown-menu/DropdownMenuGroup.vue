<script setup lang="ts">
// DropdownMenuGroup——把相关菜单项分组（屏幕阅读器朗读 group），搭配 Label 用。
import { DropdownMenuGroup, type DropdownMenuGroupProps } from 'reka-ui'

defineProps<DropdownMenuGroupProps>()
</script>

<template>
  <DropdownMenuGroup v-bind="$props">
    <slot />
  </DropdownMenuGroup>
</template>
