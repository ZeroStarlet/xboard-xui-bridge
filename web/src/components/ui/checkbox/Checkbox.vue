<script setup lang="ts">
// shadcn-vue Checkbox，基于 reka-ui CheckboxRoot/Indicator。
//
// 用法：
//
//   <Checkbox v-model="form.skip_tls_verify" id="skip-tls" />
//   <Label for="skip-tls">skip_tls_verify</Label>
//
// 与原生 input[type=checkbox] 的区别：
//   - 视觉：自定义 16x16 方框 + 勾选 √ 图标，跨浏览器一致风格（原生
//     在 macOS/Windows/Linux 各家形态不同）
//   - 三态：reka-ui 支持 boolean | 'indeterminate'，对"全选 / 部分选"场景
//     有内建语义（项目暂无此场景，但保留能力）
//   - 键盘：reka-ui 已处理 Space 切换、Enter 不触发（与原生 ARIA 约定一致）
//
// v-model 透明转发：reka-ui CheckboxRoot 消费 modelValue + 触发
// update:modelValue，与 Vue v-model 默认协议一致；本组件不写 defineModel，
// 让 useForwardPropsEmits 把整条链路直接 wire 起来。
//
// 与 .cb（style.css 内的原生 checkbox 样式类）共存：.cb 是 v-0.5 留下的
// 简化方案，配置原生 checkbox 的 accent-color；本组件是 v0.6 新组件库
// 路径，批次 10 重写 Settings 时优先用 Checkbox + Switch 组合替换 .cb。
import { computed, type HTMLAttributes } from 'vue'
import { CheckboxIndicator, CheckboxRoot, type CheckboxRootEmits, type CheckboxRootProps, useForwardPropsEmits } from 'reka-ui'
import { Check, Minus } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

interface Props extends CheckboxRootProps {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()
const emits = defineEmits<CheckboxRootEmits>()

const delegatedProps = computed(() => {
  // 仅剥 class；不剥 modelValue / defaultValue / value 等，确保 v-model 完整链路。
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { class: _omit, ...rest } = props
  return rest
})

const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <!--
    visual：data-[state=checked|indeterminate] 都加 bg-primary——三态里 false
    保持透明底（仅 border），true 与 indeterminate 都填充 primary 色让用户
    能视觉区分"未选"与"半选/已选"。indicator 内的图标按 modelValue 切换：
    'indeterminate' 显示 Minus 横线，true 显示 Check 勾，false 不渲染图标。
  -->
  <CheckboxRoot
    v-bind="forwarded"
    :class="
      cn(
        'peer size-4 shrink-0 rounded-sm border border-primary shadow-sm',
        'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
        'disabled:cursor-not-allowed disabled:opacity-50',
        'data-[state=checked]:bg-primary data-[state=checked]:text-primary-foreground',
        'data-[state=indeterminate]:bg-primary data-[state=indeterminate]:text-primary-foreground',
        props.class,
      )
    "
  >
    <CheckboxIndicator class="flex h-full w-full items-center justify-center text-current">
      <Minus v-if="props.modelValue === 'indeterminate'" class="size-3.5" />
      <Check v-else class="size-3.5" />
    </CheckboxIndicator>
  </CheckboxRoot>
</template>
