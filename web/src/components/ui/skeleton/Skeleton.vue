<script setup lang="ts">
// Skeleton：加载占位骨架。
//
// 用于异步数据加载时占据 KPI 卡 / 表格行 / 头像等位置，避免"内容突然
// 跳出"的视觉跳变（CLS - Cumulative Layout Shift）。
//
// 用法：
//
//   <div v-if="loading" class="space-y-3">
//     <Skeleton class="h-4 w-32" />     <!-- 标题占位 -->
//     <Skeleton class="h-8 w-48" />     <!-- 数值占位 -->
//   </div>
//   <div v-else>...真实内容...</div>
//
// 形状由调用方决定：组件本体只提供 muted 灰底 + animate-pulse 闪烁。
// rounded-md 是默认；要圆形头像就 rounded-full，要方块就 rounded-none。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn('animate-pulse rounded-md bg-muted', props.class),
)
</script>

<template>
  <div :class="classes" />
</template>
