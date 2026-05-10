<script setup lang="ts">
// Toast 描述段。
import { computed, type HTMLAttributes } from 'vue'
import { ToastDescription, type ToastDescriptionProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends ToastDescriptionProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
</script>

<template>
  <ToastDescription v-bind="delegatedProps" :class="cn('text-sm opacity-90', props.class)">
    <slot />
  </ToastDescription>
</template>
