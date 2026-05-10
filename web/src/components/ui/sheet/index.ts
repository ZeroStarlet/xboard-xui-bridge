// Sheet 模块 barrel——侧滑抽屉组件家族。
//
// 用法（精简版）：
//
//   <Sheet v-model:open="show">
//     <SheetContent side="right">
//       <SheetHeader>
//         <SheetTitle>...</SheetTitle>
//         <SheetDescription>...</SheetDescription>
//       </SheetHeader>
//       ...内容...
//       <SheetFooter>
//         <SheetClose as-child><Button variant="outline">取消</Button></SheetClose>
//         <Button @click="submit">保存</Button>
//       </SheetFooter>
//     </SheetContent>
//   </Sheet>
export { default as Sheet } from './Sheet.vue'
export { default as SheetTrigger } from './SheetTrigger.vue'
export { default as SheetClose } from './SheetClose.vue'
export { default as SheetContent } from './SheetContent.vue'
export { default as SheetHeader } from './SheetHeader.vue'
export { default as SheetTitle } from './SheetTitle.vue'
export { default as SheetDescription } from './SheetDescription.vue'
export { default as SheetFooter } from './SheetFooter.vue'
export { sheetVariants, type SheetVariants } from './variants'
