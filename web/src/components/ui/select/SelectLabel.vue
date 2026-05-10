<script setup lang="ts">
// SelectLabel——分组标题，不可选中、用于视觉区分子项分类。
import { computed, type HTMLAttributes } from 'vue'
import { SelectLabel, type SelectLabelProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends SelectLabelProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
</script>

<template>
  <SelectLabel v-bind="delegatedProps" :class="cn('px-2 py-1.5 text-sm font-semibold', props.class)">
    <slot />
  </SelectLabel>
</template>
