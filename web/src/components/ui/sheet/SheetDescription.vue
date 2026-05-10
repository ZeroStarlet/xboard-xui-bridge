<script setup lang="ts">
// SheetDescription——基于 reka-ui DialogDescription，screen reader 朗读
// 抽屉用途描述。
import { computed, type HTMLAttributes } from 'vue'
import { DialogDescription, type DialogDescriptionProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends DialogDescriptionProps {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})

const classes = computed(() =>
  cn('text-sm text-muted-foreground', props.class),
)
</script>

<template>
  <DialogDescription v-bind="delegatedProps" :class="classes">
    <slot />
  </DialogDescription>
</template>
