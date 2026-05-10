<script setup lang="ts">
// SelectGroup——选项分组容器，搭配 SelectLabel 做"分组名 + 子项"结构。
import { SelectGroup, type SelectGroupProps } from 'reka-ui'

defineProps<SelectGroupProps>()
</script>

<template>
  <SelectGroup v-bind="$props">
    <slot />
  </SelectGroup>
</template>
