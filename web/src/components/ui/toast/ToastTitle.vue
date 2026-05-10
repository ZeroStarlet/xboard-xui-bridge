<script setup lang="ts">
// Toast 标题段。
import { computed, type HTMLAttributes } from 'vue'
import { ToastTitle, type ToastTitleProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends ToastTitleProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
</script>

<template>
  <ToastTitle v-bind="delegatedProps" :class="cn('text-sm font-semibold', props.class)">
    <slot />
  </ToastTitle>
</template>
