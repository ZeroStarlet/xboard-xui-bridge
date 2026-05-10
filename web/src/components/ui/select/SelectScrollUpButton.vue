<script setup lang="ts">
// SelectScrollUpButton——长列表向上滚动按钮（仅在 viewport 顶部不可见时显示）。
import { computed, type HTMLAttributes } from 'vue'
import { SelectScrollUpButton, type SelectScrollUpButtonProps, useForwardProps } from 'reka-ui'
import { ChevronUp } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

interface Props extends SelectScrollUpButtonProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <SelectScrollUpButton v-bind="forwarded" :class="cn('flex cursor-default items-center justify-center py-1', props.class)">
    <ChevronUp class="size-4" />
  </SelectScrollUpButton>
</template>
