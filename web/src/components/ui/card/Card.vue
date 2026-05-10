<script setup lang="ts">
// shadcn-vue Card 根容器。
//
// 设计：纯 div 容器 + 圆角 + 边框 + 阴影。所有内部段落（CardHeader / CardTitle /
// CardDescription / CardContent / CardFooter）都是独立小组件，让模板可读性
// 更高（语义清晰胜于嵌套 div className）。
//
// 用法：
//
//   <Card>
//     <CardHeader>
//       <CardTitle>仪表盘</CardTitle>
//       <CardDescription>实时查看引擎与桥接状态</CardDescription>
//     </CardHeader>
//     <CardContent>...内容...</CardContent>
//     <CardFooter>...操作按钮...</CardFooter>
//   </Card>
//
// 与项目原 .card 类的关系：
//
//   .card 类（style.css）是 v0.5 留下的"原子化卡片"，rounded-2xl + p-6；
//   shadcn-vue 风的 Card 用 rounded-lg（var(--radius)=8px，更 Fluent）+
//   分段子元素，更模块化。两者并行：批次 8-10 重写视图时全替换为 Card 组件，
//   .card 类批次 11 之后可以下线（届时会做一次 grep 验证）。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn(
    'rounded-lg border bg-card text-card-foreground shadow-sm',
    props.class,
  ),
)
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
