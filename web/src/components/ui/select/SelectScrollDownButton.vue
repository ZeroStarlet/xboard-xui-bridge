<script setup lang="ts">
// SelectScrollDownButton——长列表向下滚动按钮（仅在 viewport 底部不可见时显示）。
import { computed, type HTMLAttributes } from 'vue'
import { SelectScrollDownButton, type SelectScrollDownButtonProps, useForwardProps } from 'reka-ui'
import { ChevronDown } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

interface Props extends SelectScrollDownButtonProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <SelectScrollDownButton v-bind="forwarded" :class="cn('flex cursor-default items-center justify-center py-1', props.class)">
    <ChevronDown class="size-4" />
  </SelectScrollDownButton>
</template>
