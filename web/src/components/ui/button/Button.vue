<script setup lang="ts">
// shadcn-vue Button 组件本体（v0.6 Fluent + 语义 token）。
//
// 用法示例：
//
//   <Button>保存</Button>
//   <Button variant="destructive" size="sm">删除</Button>
//   <Button variant="outline" :disabled="loading">
//     <Loader2 v-if="loading" class="animate-spin" />
//     {{ loading ? '加载中…' : '刷新' }}
//   </Button>
//
// 设计要点：
//
//   - 默认渲染 <button type="button"> —— 不是 type="submit"，避免被误用为
//     form 默认提交按钮（HTML 默认 type=submit 是历史遗留陷阱）。
//     需要提交按钮的场景显式 :type="'submit'"。
//
//   - props.class 透传：消费者通过 :class="..." 追加的类经过 cn() 合并到
//     cva 输出之后；tailwind-merge 让 utility 冲突时调用方更高优先级
//     （详见 lib/utils.ts cn 注释的 5 种 case）。
//
//   - $attrs 透传：未在 props 里声明的属性（aria-label / type / title /
//     v-on:click 等）由 Vue 默认透传到根元素 <button>。inheritAttrs 默认
//     即可，不需显式 false。
//
//   - 不暴露 asChild：shadcn-vue 官方组件用 reka-ui Primitive 提供 asChild
//     让 <Button as="a" href="...">、<Button as-child><RouterLink /></Button>
//     这类用法成立。本项目用例少（绝大多数按钮就是 button），加 asChild 反而
//     让组件 API 变重；若将来有 RouterLink-as-button 需求，复用 cn(buttonVariants(...))
//     生成 class 贴到 RouterLink 上即可（这正是 buttonVariants 单独导出的目的）。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'
// 从 ./variants 直接 import，避免组件本体 ⇄ barrel index.ts 自循环依赖。
// 详见 variants.ts 文件头注释。
import { buttonVariants, type ButtonVariants } from './variants'

interface Props {
  /** 视觉变体——颜色 / 边框 / hover 风格的组合。详见 index.ts buttonVariants. */
  variant?: ButtonVariants['variant']
  /** 尺寸——影响高度 / 内边距 / 字号。 */
  size?: ButtonVariants['size']
  /** 调用方追加的 class，会与组件默认类经 cn() 合并。 */
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

// 把动态 class 计算成 ref，让模板里 :class="classes" 简洁。
// 这里不直接 :class="cn(...)" 是为了让模板更易读 + 静态分析更友好。
const classes = computed(() =>
  cn(buttonVariants({ variant: props.variant, size: props.size }), props.class),
)
</script>

<template>
  <!-- type="button" 显式设定：避免在 <form> 里被当作 submit 触发整个表单提交。
       需要提交按钮的场景调用方显式 type="submit" 覆盖即可。 -->
  <button type="button" :class="classes">
    <slot />
  </button>
</template>
