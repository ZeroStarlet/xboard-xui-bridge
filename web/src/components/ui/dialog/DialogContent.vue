<script setup lang="ts">
// DialogContent——中心模态主体，含 Overlay + Portal + Content + 内置 X 关闭。
//
// 与 SheetContent 的差异：
//   - 居中定位（top-1/2 left-1/2 -translate-1/2）而非边缘对齐
//   - zoom-in / zoom-out 动画（视觉聚焦"决策"），而非 slide
//   - 默认宽度 max-w-lg（32rem），决策对话内容通常不需要太宽
//
// rounded 用 lg 与 v0.6 Fluent 8px 圆角一致。
import { computed, type HTMLAttributes } from 'vue'
import {
  DialogClose,
  DialogContent,
  DialogOverlay,
  DialogPortal,
  type DialogContentEmits,
  type DialogContentProps,
  useForwardPropsEmits,
} from 'reka-ui'
import { X } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

interface Props extends DialogContentProps {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()
const emits = defineEmits<DialogContentEmits>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <DialogPortal>
    <DialogOverlay
      class="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm
             data-[state=open]:animate-in data-[state=closed]:animate-out
             data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <DialogContent
      v-bind="forwarded"
      :class="
        cn(
          'fixed left-1/2 top-1/2 z-50 grid w-full max-w-lg -translate-x-1/2 -translate-y-1/2 gap-4',
          'border bg-background p-6 shadow-lg sm:rounded-lg',
          'duration-200',
          'data-[state=open]:animate-in data-[state=closed]:animate-out',
          'data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
          'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
          props.class,
        )
      "
    >
      <slot />
      <DialogClose
        class="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity
               hover:opacity-100
               focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2
               disabled:pointer-events-none
               data-[state=open]:bg-accent data-[state=open]:text-muted-foreground"
      >
        <X class="size-4" />
        <span class="sr-only-soft">Close</span>
      </DialogClose>
    </DialogContent>
  </DialogPortal>
</template>
