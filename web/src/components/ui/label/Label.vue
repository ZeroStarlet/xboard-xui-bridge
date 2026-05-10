<script setup lang="ts">
// shadcn-vue Label 组件（v0.6）。
//
// 设计选择：用原生 <label> 而非 reka-ui Label primitive。
//
//   reka-ui 的 Label 主要功能是把"点击 label 文字让光标进入对应 input"
//   的行为处理成 component-aware（在自定义复合组件里也能正确聚焦内部
//   原生 input）。本项目所有 input 都是原生 <input>，原生 <label>
//   配合 for="id" 已经完成相同行为，无需引入 primitive。
//
//   peer-disabled 联动样式：本组件用 Tailwind 的 peer-disabled: 修饰符
//   实现——在结构上把 <Label> 紧邻 <Input> 时，Input 的 disabled 态
//   会让 Label 文字变灰。这要求标签结构是 <label><input class="peer" /></label>
//   或 <input class="peer" /><label>（label 在 input 之后）。当前项目
//   默认结构是 <label /><input />（label 在前），所以 peer-disabled
//   不会自动联动；调用方需要时再手工 :class="form.disabled ? '...' : ''"。
//
// 用法：
//
//   <Label for="username">用户名</Label>
//   <Input id="username" v-model="username" />
//
//   for="..." 与 Input id="..." 必须配对——确保点击文字聚焦输入框、
//   屏幕阅读器朗读字段名。这是 WCAG 1.3.1 / 3.3.2 的硬性要求。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  /** 关联的 input id，必须与下方 Input 的 id prop 一致。 */
  for?: string
  /** 外部追加的 class。 */
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn(
    'text-sm font-medium leading-none',
    // peer-disabled 联动：当邻近的 .peer 元素 disabled 时，label 也变灰
    'peer-disabled:cursor-not-allowed peer-disabled:opacity-70',
    props.class,
  ),
)
</script>

<template>
  <label :for="props.for" :class="classes">
    <slot />
  </label>
</template>
