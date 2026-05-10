<script setup lang="ts">
// SelectSeparator——选项之间的分隔线。
import { computed, type HTMLAttributes } from 'vue'
import { SelectSeparator, type SelectSeparatorProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends SelectSeparatorProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
</script>

<template>
  <SelectSeparator v-bind="delegatedProps" :class="cn('-mx-1 my-1 h-px bg-muted', props.class)" />
</template>
