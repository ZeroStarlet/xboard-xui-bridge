<script setup lang="ts">
// Dialog 根组件——透传到 reka-ui DialogRoot。中心模态语义，强阻断决策。
//
// 与 Sheet 的语义区别：
//   - Dialog：中心居中 + zoom-in 动画 + 模态遮罩，"请用户做决定"（删除确认 /
//     登出确认 / 关键警告）
//   - Sheet：边缘滑入，"打开侧栏工作流"（编辑表单 / 详情查看）
//
// 批次 9 用 Dialog 做删除桥接的二次确认（替代原 native confirm，提供更好
// 的一致性与样式定制）。
import { DialogRoot, type DialogRootProps, type DialogRootEmits, useForwardPropsEmits } from 'reka-ui'

const props = defineProps<DialogRootProps>()
const emits = defineEmits<DialogRootEmits>()

const forwarded = useForwardPropsEmits(props, emits)
</script>

<template>
  <DialogRoot v-bind="forwarded">
    <slot />
  </DialogRoot>
</template>
