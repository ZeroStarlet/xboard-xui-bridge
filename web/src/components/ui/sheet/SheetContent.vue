<script setup lang="ts">
// SheetContent——抽屉主体内容，包含 Overlay + Portal + Content + 内置关闭按钮。
//
// 依赖父级 Sheet（DialogRoot）控制开关状态；side prop 决定滑出方向，详见
// variants.ts。
//
// Portal：reka-ui Dialog 的 portal 把 content 渲染到 body 末尾（脱离当前
// dom 树），避免父元素的 overflow:hidden / transform 截断弹层。配合
// fixed 定位让抽屉始终覆盖视窗。
//
// Overlay：fixed 全屏半透明背景 + backdrop-blur，增强层级与"模态感"。
// 点击 overlay 默认关闭抽屉（reka-ui 行为，可通过 :on-pointer-down-outside
// 拦截）。
//
// 关闭按钮：右上角 X 图标，绝对定位到 content 内角。aria-label 由 i18n 上层
// 传入；本组件给出 'Close' 默认 fallback——批次 8-10 重写视图时改用 t() 注入。
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
import { sheetVariants, type SheetVariants } from './variants'

interface Props extends DialogContentProps {
  /** 抽屉滑出方向（default 'right'）。 */
  side?: SheetVariants['side']
  class?: HTMLAttributes['class']
}

const props = withDefaults(defineProps<Props>(), {
  side: 'right',
})

const emits = defineEmits<DialogContentEmits>()

// 把 props 中本组件包装的字段（class / side）剥掉再透传到 reka 的
// DialogContent，避免它当作未知 prop 投递给底层 div。
const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omitClass, side: _omitSide, ...rest } = props
  return rest
})

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <DialogPortal>
    <!-- Overlay：fixed 全屏半透明背景 + backdrop-blur -->
    <DialogOverlay
      class="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm
             data-[state=open]:animate-in data-[state=closed]:animate-out
             data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <!-- Content：按 side 决定滑入方向；shadcn-vue 默认带 close button -->
    <DialogContent
      v-bind="forwarded"
      :class="cn(sheetVariants({ side }), props.class)"
    >
      <slot />
      <!-- 右上角关闭按钮——使用语义 close ring，键盘 Escape 由 DialogContent 处理 -->
      <DialogClose
        class="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity
               hover:opacity-100
               focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2
               disabled:pointer-events-none
               data-[state=open]:bg-secondary"
      >
        <X class="size-4" />
        <span class="sr-only-soft">Close</span>
      </DialogClose>
    </DialogContent>
  </DialogPortal>
</template>
