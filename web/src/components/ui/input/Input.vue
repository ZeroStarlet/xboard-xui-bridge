<script setup lang="ts">
// shadcn-vue Input 组件（v0.6）。
//
// 设计要点：
//
//   1. v-model 桥接：通过 defineModel<string | number | null>() 自动支持
//      v-model 双向绑定（Vue 3.4+ 的标准模式）。等价于:
//
//        defineProps<{ modelValue?: string | number | null }>()
//        defineEmits<{ 'update:modelValue': [value: string | number | null] }>()
//
//      但 defineModel 把这两步合一，模板里用 v-model="..." 透明工作。
//      类型联合 string | number | null 兼容三种常见 v-model 用法：
//        - 普通文本 v-model="username"           → string
//        - 数字 v-model.number="form.timeout"   → number
//        - 可清空字段 v-model="optional"        → string | null
//
//   2. 不区分变体：与 shadcn-vue 官方一致，Input 没有 variants（不像 Button），
//      所有"危险态/成功态"通过外层包装实现（例如 aria-invalid + 红色 ring）。
//
//   3. type 默认 'text'：消费者可传 'password' / 'number' / 'email' 等，
//      不在 props 里强类型枚举——因为 HTMLInputElement 可接受的 type 值
//      多达 22 种，硬编码会让维护成本高于收益。$attrs 透传足够。
//
//   4. 焦点态用 ring（不是 box-shadow）：与 Button 焦点环视觉一致，深色模式
//      下的 ring-offset-background 也跟随主题切换。
//
//   5. file 类型 input 的特殊处理：file:* utilities 让浏览器原生"选择文件"
//      按钮也跟随主题色与尺寸 —— 否则该按钮在深色模式下保留浅色系统样式
//      显得突兀。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  /** 外部追加的 class。 */
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

// defineModel：双向绑定 v-model="..." 的标准 Vue 3.4+ 写法。
// 类型联合 string | number | null：
//   - string：普通文本（v-model="username"）
//   - number：v-model.number 修饰符自动 parseFloat，写回数字
//   - null：可清空字段（设置为 null 触发清空逻辑）
// 三类 model 共用同一组件，模板里的 v-model 透明工作。
const model = defineModel<string | number | null>()

const classes = computed(() =>
  cn(
    // 基础类：高度对齐 Button h-9，确保表单内 Input + Button 等高
    'flex h-9 w-full rounded-md border border-input bg-background',
    'px-3 py-1 text-sm shadow-sm transition-colors',
    'placeholder:text-muted-foreground',
    // file 类型按钮主题化
    'file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground',
    // 焦点环——ring-offset-background 让深色模式下 ring 与 input 间的间隙
    // 跟随主题（不再露出突兀白边），与 Button / .cb 复选框策略一致
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
    'focus-visible:ring-offset-2 focus-visible:ring-offset-background',
    // 禁用态
    'disabled:cursor-not-allowed disabled:opacity-50',
    // 错误态：父级或同级写 aria-invalid="true" 时自动红框
    'aria-[invalid=true]:border-destructive aria-[invalid=true]:focus-visible:ring-destructive',
    props.class,
  ),
)
</script>

<template>
  <input v-model="model" :class="classes" />
</template>
