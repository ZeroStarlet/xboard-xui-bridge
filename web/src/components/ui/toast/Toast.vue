<script setup lang="ts">
// 单条 Toast——基于 reka-ui ToastRoot。
//
// reka-ui ToastRoot 已处理：
//   - 自动消失（duration prop）
//   - 鼠标悬停暂停计时
//   - swipe-to-dismiss 手势（移动端右滑关闭）
//   - data-[state=open|closed] 状态切换
//   - update:open 事件（用于 v-model:open 与 useToast.dismiss 联动）
//
// 本组件只负责"包样式 + 透传变体"。
import { computed, type HTMLAttributes } from 'vue'
import { ToastRoot, type ToastRootEmits, type ToastRootProps, useForwardPropsEmits } from 'reka-ui'
import { cn } from '@/lib/utils'
import { toastVariants, type ToastVariants } from './variants'

interface Props extends ToastRootProps {
  variant?: ToastVariants['variant']
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()
const emits = defineEmits<ToastRootEmits>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omitClass, variant: _omitVariant, ...rest } = props
  return rest
})

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <ToastRoot
    v-bind="forwarded"
    :class="cn(toastVariants({ variant: props.variant }), props.class)"
  >
    <slot />
  </ToastRoot>
</template>
