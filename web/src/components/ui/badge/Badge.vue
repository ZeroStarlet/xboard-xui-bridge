<script setup lang="ts">
// Badge 组件本体——状态标签 / pill。
//
// 用法：
//
//   <Badge>默认</Badge>
//   <Badge variant="success">启用</Badge>
//   <Badge variant="warning">未完整</Badge>
//   <Badge variant="destructive">已停止</Badge>
//
// 与点状指示器搭配（保留原 .pill-dot 样式语义）：
//
//   <Badge variant="success">
//     <span class="size-1.5 rounded-full bg-brand-500" aria-hidden="true" />
//     启用
//   </Badge>
//
//   slot 内自由组合点 + 文字，不在组件本体硬编码——保持灵活，避免每加
//   一种状态就改 Badge prop 的 API 膨胀。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'
// 从 ./variants 直接 import，避免组件 ⇄ barrel 自循环。
import { badgeVariants, type BadgeVariants } from './variants'

interface Props {
  variant?: BadgeVariants['variant']
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn(badgeVariants({ variant: props.variant }), props.class),
)
</script>

<template>
  <span :class="classes">
    <slot />
  </span>
</template>
