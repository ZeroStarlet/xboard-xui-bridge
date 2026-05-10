<script setup lang="ts">
// ToastViewport——所有 toast 的容器，固定在屏幕角落。
//
// 默认右下角；移动端（sm 以下）改为顶部中央，避免与底部 tab 栏冲突。
// z-[100] 高于 dialog/sheet 的 z-50，确保即使有模态打开 toast 仍可见。
import { computed, type HTMLAttributes } from 'vue'
import { ToastViewport, type ToastViewportProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends ToastViewportProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
</script>

<template>
  <ToastViewport
    v-bind="delegatedProps"
    :class="
      cn(
        'fixed top-0 z-[100] flex max-h-screen w-full flex-col-reverse p-4',
        'sm:bottom-0 sm:right-0 sm:top-auto sm:flex-col md:max-w-[420px]',
        props.class,
      )
    "
  />
</template>
