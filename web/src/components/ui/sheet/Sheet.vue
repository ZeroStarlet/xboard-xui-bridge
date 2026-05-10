<script setup lang="ts">
// Sheet 根组件——直接转发到 reka-ui DialogRoot。
//
// 用法：
//
//   <Sheet v-model:open="show">
//     <SheetTrigger as-child>
//       <Button>打开抽屉</Button>
//     </SheetTrigger>
//     <SheetContent side="right">
//       <SheetHeader>
//         <SheetTitle>编辑桥接</SheetTitle>
//         <SheetDescription>填写完成后立即触发引擎热重载。</SheetDescription>
//       </SheetHeader>
//       ... 表单 ...
//       <SheetFooter>
//         <SheetClose as-child>
//           <Button variant="outline">取消</Button>
//         </SheetClose>
//         <Button @click="submit">保存</Button>
//       </SheetFooter>
//     </SheetContent>
//   </Sheet>
//
// 默认是受控的：v-model:open="..." 让父级控制开关；不传 v-model 时由
// SheetTrigger 自动管理状态（reka-ui 内部维持 open ref）。
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
