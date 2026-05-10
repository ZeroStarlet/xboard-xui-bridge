<script setup lang="ts">
// DropdownMenu 根——透传到 reka-ui DropdownMenuRoot。
//
// 用法：
//
//   <DropdownMenu>
//     <DropdownMenuTrigger as-child>
//       <Button variant="ghost">外观</Button>
//     </DropdownMenuTrigger>
//     <DropdownMenuContent>
//       <DropdownMenuLabel>主题</DropdownMenuLabel>
//       <DropdownMenuItem @select="setTheme('light')">亮色</DropdownMenuItem>
//       <DropdownMenuItem @select="setTheme('dark')">深色</DropdownMenuItem>
//       <DropdownMenuSeparator />
//       <DropdownMenuItem @select="setTheme('system')">跟随系统</DropdownMenuItem>
//     </DropdownMenuContent>
//   </DropdownMenu>
//
// reka-ui 已处理：
//   - 键盘导航（方向键 / Home / End / Esc / type-ahead）
//   - 焦点循环（在 menu 内 Tab）
//   - aria-haspopup="menu" + aria-expanded 同步
//   - data-[state=open|closed] / data-[side]
import {
  DropdownMenuRoot,
  type DropdownMenuRootEmits,
  type DropdownMenuRootProps,
  useForwardPropsEmits,
} from 'reka-ui'

const props = defineProps<DropdownMenuRootProps>()
const emits = defineEmits<DropdownMenuRootEmits>()

const forwarded = useForwardPropsEmits(props, emits)
</script>

<template>
  <DropdownMenuRoot v-bind="forwarded">
    <slot />
  </DropdownMenuRoot>
</template>
