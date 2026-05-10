<script setup lang="ts">
// Toast 右上角 X 关闭按钮。group-hover/group-focus 让按钮默认半透明，
// 鼠标悬停 toast 时变实——避免常态下 toast 边角太 busy。
import { computed, type HTMLAttributes } from 'vue'
import { ToastClose, type ToastCloseProps } from 'reka-ui'
import { X } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

interface Props extends ToastCloseProps { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})
</script>

<template>
  <ToastClose
    v-bind="delegatedProps"
    :class="
      cn(
        'absolute right-1 top-1 rounded-md p-1 text-foreground/50 opacity-0 transition-opacity',
        'hover:text-foreground focus:opacity-100 focus:outline-none focus:ring-2 group-hover:opacity-100',
        'group-[.destructive]:text-red-300 group-[.destructive]:hover:text-red-50 group-[.destructive]:focus:ring-red-400 group-[.destructive]:focus:ring-offset-red-600',
        props.class,
      )
    "
  >
    <X class="size-4" />
  </ToastClose>
</template>
