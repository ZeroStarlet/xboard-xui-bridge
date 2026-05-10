<script setup lang="ts">
// DropdownMenuItem——单条菜单项。可选 inset prop 在左侧多留 8 padding（搭配
// 选中标记图标用）。
//
// data-[highlighted] 是键盘焦点态；data-[disabled] 自动禁用且变灰。
// @select 事件在 reka-ui 由 Item 触发（点击或 Enter/Space），与原生 click 等价
// 但 type-safe（emit 类型由 reka-ui 提供）。
import { computed, type HTMLAttributes } from 'vue'
import { DropdownMenuItem, type DropdownMenuItemProps, useForwardProps } from 'reka-ui'
import { cn } from '@/lib/utils'

interface Props extends DropdownMenuItemProps {
  /** 在左侧增加 pl-8 间距，搭配选中标记图标布局用。 */
  inset?: boolean
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omitClass, inset: _omitInset, ...rest } = props
  return rest
})

const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <DropdownMenuItem
    v-bind="forwarded"
    :class="
      cn(
        'relative flex cursor-default select-none items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-none transition-colors',
        'focus:bg-accent focus:text-accent-foreground',
        'data-[disabled]:pointer-events-none data-[disabled]:opacity-50',
        '[&>svg]:size-4 [&>svg]:shrink-0',
        inset && 'pl-8',
        props.class,
      )
    "
  >
    <slot />
  </DropdownMenuItem>
</template>
