<script setup lang="ts">
// TooltipContent——气泡内容，Portal 渲染避免被 overflow 裁剪。
//
// sideOffset=4：与 trigger 之间留 4px。
import { computed, type HTMLAttributes } from 'vue'
import {
  TooltipContent,
  TooltipPortal,
  type TooltipContentEmits,
  type TooltipContentProps,
  useForwardPropsEmits,
} from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends TooltipContentProps {
  class?: HTMLAttributes['class']
}

const props = withDefaults(defineProps<Props>(), {
  sideOffset: 4,
})

const emits = defineEmits<TooltipContentEmits>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <TooltipPortal>
    <TooltipContent
      v-bind="forwarded"
      :class="
        cn(
          'z-50 overflow-hidden rounded-md border bg-popover px-3 py-1.5 text-xs text-popover-foreground shadow-md',
          // 入场动画覆盖 reka-ui Tooltip 的两种 open 状态：
          //   - delayed-open：默认延迟（700ms hover 后）打开，常态
          //   - instant-open：键盘 focus 触发 / skipDelayDuration 内连续打开，
          //     不走延迟。两者都需要相同入场动画，否则键盘用户看到 tooltip
          //     瞬间出现而无淡入感（视觉跳变）。
          'data-[state=delayed-open]:animate-in data-[state=instant-open]:animate-in data-[state=closed]:animate-out',
          'data-[state=closed]:fade-out-0 data-[state=delayed-open]:fade-in-0 data-[state=instant-open]:fade-in-0',
          'data-[state=closed]:zoom-out-95 data-[state=delayed-open]:zoom-in-95 data-[state=instant-open]:zoom-in-95',
          'data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2',
          'data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2',
          props.class,
        )
      "
    >
      <slot />
    </TooltipContent>
  </TooltipPortal>
</template>
