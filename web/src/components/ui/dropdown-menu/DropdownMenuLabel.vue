<script setup lang="ts">
// DropdownMenuLabel——分组标题，不可选中。
import { computed, type HTMLAttributes } from 'vue'
import { DropdownMenuLabel, type DropdownMenuLabelProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends DropdownMenuLabelProps {
  inset?: boolean
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omitClass, inset: _omitInset, ...rest } = props
  return rest
})
</script>

<template>
  <DropdownMenuLabel
    v-bind="delegatedProps"
    :class="cn('px-2 py-1.5 text-xs font-semibold text-muted-foreground', inset && 'pl-8', props.class)"
  >
    <slot />
  </DropdownMenuLabel>
</template>
