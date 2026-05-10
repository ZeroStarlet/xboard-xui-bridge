// Toast 模块 barrel——非阻塞通知组件家族 + Toaster 全局编排器。
//
// 用法：
//
//   1. 在 App.vue 根级挂一次 <Toaster />（批次 7 完成）
//   2. 业务代码调 useToast() 触发：
//
//      const { toast } = useToast()
//      toast({ title: '已保存', description: '配置生效', variant: 'success' })
//      toast({ title: '操作失败', description: e.message, variant: 'destructive' })
//
//   3. duration: 0 = 永不自动消失（仅严重错误）
export { default as Toast } from './Toast.vue'
export { default as ToastTitle } from './ToastTitle.vue'
export { default as ToastDescription } from './ToastDescription.vue'
export { default as ToastClose } from './ToastClose.vue'
export { default as ToastViewport } from './ToastViewport.vue'
export { default as ToastProvider } from './ToastProvider.vue'
export { default as Toaster } from './Toaster.vue'
export { toastVariants, type ToastVariants } from './variants'
// 便捷再导出 useToast，让消费者一次 import 就能拿到组件 + composable
export { useToast } from '@/composables/useToast'
