<script setup lang="ts">
// SheetTitle——基于 reka-ui DialogTitle，可访问性必须存在
// （screen reader 通过它定位抽屉名称）。
import { computed, type HTMLAttributes } from 'vue'
import { DialogTitle, type DialogTitleProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends DialogTitleProps {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})

const classes = computed(() =>
  cn('text-lg font-semibold text-foreground', props.class),
)
</script>

<template>
  <DialogTitle v-bind="delegatedProps" :class="classes">
    <slot />
  </DialogTitle>
</template>
