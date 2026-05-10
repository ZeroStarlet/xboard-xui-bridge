<script setup lang="ts">
// TableCell——<td>，表体单元格。p-2 与 TableHead 的 px-2 一致（垂直方向行高
// 由父级 tr 决定）。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() =>
  cn(
    'p-2 align-middle',
    '[&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]',
    props.class,
  ),
)
</script>

<template>
  <td :class="classes">
    <slot />
  </td>
</template>
