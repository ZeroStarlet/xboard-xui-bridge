<script setup lang="ts">
// CardTitle：Card 标题。
//
// 渲染为 <h3>——card 在大多数场景是 section 的子元素，section 标题级别
// 通常对应文档大纲层级 2-3 级。h3 是个稳妥折中：太高（h1/h2）会与页面
// 主标题冲突，太低（h5/h6）SEO 与屏幕阅读器导航树扁平化收益降低。
//
// 调用方需要其它语义层级时通过 :as="'h2'" 或外层包 <section><h2><CardTitle/></h2></section>
// 实现——这里不暴露 :as prop 是因为 99% 用例是 h3，引入 prop 只为 1% 场景
// 不划算。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn('text-base font-semibold leading-none tracking-tight text-foreground', props.class),
)
</script>

<template>
  <h3 :class="classes">
    <slot />
  </h3>
</template>
