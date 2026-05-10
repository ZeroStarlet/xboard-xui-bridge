<script setup lang="ts">
// AlertTitle：Alert 内的标题段。
//
// 渲染为 <h5> —— Alert 通常在主 section 里出现，标题层级 5 较低，避免与
// 页面 h2/h3 标题冲突。
//
// 关键：col-start-2 是必须的，与父级 Alert 的 has-[>svg] 条件 grid 配合：
//
//   - 无 svg 时：父级 grid-cols-[0_1fr]（col 1 宽度 0），AlertTitle col-start-2
//     直接跳到 col 2 占满全宽；同时父级无 has-[>svg]:gap-x-3，col 1 与 col 2
//     间无 gap，视觉上无缩进。
//   - 有 svg 时：父级 grid-cols-[auto_1fr]，svg 在 col 1，AlertTitle col-start-2
//     在 col 2，has-[>svg]:gap-x-3 让两列间留 12px 间距。
//
// 这是 shadcn-vue 官方 Alert 的标准模式：父级条件 grid 决定列宽与列间距，
// 子级 col-start-2 锁定文字落在第 2 列；两者缺一会导致 grid auto-placement
// 把多个子节点错位排列（v0.6 初版踩过这个坑，详见批次 4 Codex 第 2 轮 review）。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn('col-start-2 line-clamp-1 min-h-4 font-medium tracking-tight', props.class),
)
</script>

<template>
  <h5 :class="classes">
    <slot />
  </h5>
</template>
