<script setup lang="ts">
// DialogDescription——基于 reka-ui DialogDescription。
import { computed, type HTMLAttributes } from 'vue'
import { DialogDescription, type DialogDescriptionProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends DialogDescriptionProps { class?: HTMLAttributes['class'] }
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
