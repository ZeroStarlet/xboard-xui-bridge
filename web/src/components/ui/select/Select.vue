<script setup lang="ts">
// Select 根组件——透传到 reka-ui SelectRoot。
//
// 用法（替代 <select>）：
//
//   <Select v-model="form.protocol">
//     <SelectTrigger>
//       <SelectValue placeholder="选择协议" />
//     </SelectTrigger>
//     <SelectContent>
//       <SelectItem v-for="p in protocols" :key="p" :value="p">{{ p }}</SelectItem>
//     </SelectContent>
//   </Select>
//
// 与原生 <select> 的区别：
//   - 视觉跨浏览器一致（macOS/Windows/Linux 原生 select 形态差异巨大）
//   - 支持完整键盘导航（Enter 打开 / 方向键 / type-ahead 首字母搜索 / Esc 关闭）
//   - 支持自定义内容（图标 / 副标题 / 分组），原生 <select> 选项只能纯文本
//   - data-[state] / data-[side] / data-[align] 属性供 Tailwind 状态选择器
//
// 代价：bundle +~6KB（reka-ui Select primitive），项目对 select 使用量
// 不大（仅 Bridges form 的 protocol / xboard_type 两处），可接受。
import { SelectRoot, type SelectRootEmits, type SelectRootProps, useForwardPropsEmits } from 'reka-ui'

const props = defineProps<SelectRootProps>()
const emits = defineEmits<SelectRootEmits>()

const forwarded = useForwardPropsEmits(props, emits)
</script>

<template>
  <SelectRoot v-bind="forwarded">
    <slot />
  </SelectRoot>
</template>
