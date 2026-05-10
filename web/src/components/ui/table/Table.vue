<script setup lang="ts">
// Table 根容器——包装 <table> 在可滚动 div 内，保证窄屏不溢出。
//
// 与项目原 .data-table（style.css）的关系：.data-table 是 v0.5 留下的 utility
// class，可直接在 <table> 上用；Table 组件是 v0.6 shadcn-vue 风的语义化包装，
// 提供 TableHeader/Row/Cell 等子组件，调用方写 <Table><TableHeader>... 比贴
// "data-table" string 更类型安全 + 模板可读。两者并存，批次 9-10 会渐进替换。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn('w-full caption-bottom text-sm', props.class),
)
</script>

<template>
  <!--
    relative + w-full + overflow-auto：让长表格在窄屏内可水平滚动而不撑破布局。
    内层 <table> 由 props.class 控制底色 / 字号；外层不接 props 控制
    （overflow 行为是固定逻辑，不应被覆盖）。
  -->
  <div class="relative w-full overflow-auto">
    <table :class="classes">
      <slot />
    </table>
  </div>
</template>
