<script setup lang="ts">
// SelectTrigger——按钮形态的触发器，按下打开 SelectContent。
//
// 视觉对齐 Input：高度 h-9、border、rounded-md、focus 环用 ring 语义 token。
// 右侧带 ChevronDown 图标提示"可下拉"，group-data-[state=open]:rotate-180 让
// 箭头在打开时翻转——data-state 属性挂在 SelectTrigger 根节点上，svg 是
// SelectIcon 的子节点，必须用 group + group-data-* 父状态选择器才能让 svg
// 响应 trigger 的 open 态（直接在 svg 上写 data-[state=open] 不会生效，因为
// data-state 不在 svg 自身上——批次 5 Codex review 第 1 轮指出过这个坑）。
import { computed, type HTMLAttributes } from 'vue'
import { SelectIcon, SelectTrigger, type SelectTriggerProps, useForwardProps } from 'reka-ui'
import { ChevronDown } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

interface Props extends SelectTriggerProps {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const delegatedProps = computed(() => {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})

const forwarded = useForwardProps(delegatedProps)
</script>

<template>
  <SelectTrigger
    v-bind="forwarded"
    :class="
      cn(
        'group flex h-9 w-full items-center justify-between whitespace-nowrap',
        'rounded-md border border-input bg-background px-3 py-1 text-sm shadow-sm',
        'placeholder:text-muted-foreground',
        'focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-background',
        'disabled:cursor-not-allowed disabled:opacity-50',
        '[&>span]:line-clamp-1',
        props.class,
      )
    "
  >
    <slot />
    <SelectIcon as-child>
      <ChevronDown class="size-4 opacity-50 transition-transform group-data-[state=open]:rotate-180" />
    </SelectIcon>
  </SelectTrigger>
</template>
