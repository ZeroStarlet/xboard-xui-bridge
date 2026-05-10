<script setup lang="ts">
// TableHead——<th>，表头单元格。h-10 + 左对齐文字 + muted-foreground 文字色。
//
// [&:has([role=checkbox])]:pr-0：当表头单元格包含 checkbox 时去掉右 padding，
// 让全选 checkbox 与下面行的 checkbox 视觉对齐——shadcn-vue 表格选中模式
// 的标准做法（项目暂未用，但保留方便后续扩展）。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() =>
  cn(
    'h-10 px-2 text-left align-middle font-medium text-muted-foreground',
    '[&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]',
    props.class,
  ),
)
</script>

<template>
  <th :class="classes">
    <slot />
  </th>
</template>
