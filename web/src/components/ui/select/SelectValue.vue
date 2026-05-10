<script setup lang="ts">
// SelectValue——展示当前选中项的文字。
// reka-ui 内部根据 placeholder 与 model 决定 fallback 渲染。
import { SelectValue, type SelectValueProps } from 'reka-ui'

defineProps<SelectValueProps>()
</script>

<template>
  <SelectValue v-bind="$props">
    <slot />
  </SelectValue>
</template>
