<script setup lang="ts">
// Separator：水平/垂直分割线。
//
// 默认水平、装饰性（aria-orientation + role 自动设置）。
//
// 用法：
//
//   <!-- 默认水平 -->
//   <Separator />
//
//   <!-- 垂直，在 flex 行内分隔图标按钮组 -->
//   <Separator orientation="vertical" class="h-6" />
//
// 装饰性 vs 语义性：
//
//   - 默认 :decorative="true"：role="none"，屏幕阅读器跳过——大多数装饰
//     用途（卡片内分段、列表项之间）选这个，避免阅读器朗读"分隔符"噪声
//   - :decorative="false"：role="separator"，屏幕阅读器会朗读"分隔符"，
//     用于"前后内容语义独立"的真正分组场景
//
// 选择 native <div role="..."> 而非 reka-ui Separator primitive：
//   reka 的 Separator 主要价值是 keyboard navigation 跳过（与 toolbar
//   primitive 联动），本项目没有用 toolbar primitive，纯视觉分隔用 div
//   足够。少一个依赖、少一层 wrapper。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  /** 'horizontal' 默认 / 'vertical'。垂直时高度由调用方 :class="'h-6'" 指定。 */
  orientation?: 'horizontal' | 'vertical'
  /**
   * 装饰性分隔（true=role="none"，屏幕阅读器跳过）vs 语义性
   * （false=role="separator"，朗读"分隔符"）。默认 true 适合大多数视觉分隔。
   */
  decorative?: boolean
  class?: HTMLAttributes['class']
}

const props = withDefaults(defineProps<Props>(), {
  orientation: 'horizontal',
  decorative: true,
})

const classes = computed(() =>
  cn(
    'shrink-0 bg-border',
    props.orientation === 'horizontal' ? 'h-px w-full' : 'h-full w-px',
    props.class,
  ),
)

const role = computed(() => (props.decorative ? 'none' : 'separator'))
const ariaOrientation = computed(() =>
  props.decorative ? undefined : props.orientation,
)
</script>

<template>
  <div :role="role" :aria-orientation="ariaOrientation" :class="classes" />
</template>
