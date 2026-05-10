<script setup lang="ts">
// Toaster——全局 toast 编排器。挂载在 App.vue 根布局，监听 useToast 的
// toasts 队列，按顺序渲染每条 toast。
//
// 与 useToast 的协作：
//   - useToast.toast({...}) 往队列追加；本组件 v-for 自动渲染新 toast
//   - reka-ui ToastRoot 自动消失时 emit update:open=false；
//     这里通过 @update:open 桥接到 useToast.dismiss(id)，让本地 ref 状态
//     与 reka 的内部 open ref 保持同步
//
// 用法（仅在 App.vue 根级实例化一次）：
//
//   <Toaster />
import { useToast } from '@/composables/useToast'
import Toast from './Toast.vue'
import ToastTitle from './ToastTitle.vue'
import ToastDescription from './ToastDescription.vue'
import ToastClose from './ToastClose.vue'
import ToastProvider from './ToastProvider.vue'
import ToastViewport from './ToastViewport.vue'

const { toasts, dismiss } = useToast()
</script>

<template>
  <ToastProvider>
    <Toast
      v-for="t in toasts"
      :key="t.id"
      :variant="t.variant"
      :duration="t.duration"
      :open="t.open"
      @update:open="(open) => { if (!open) dismiss(t.id) }"
    >
      <div class="grid gap-1">
        <ToastTitle v-if="t.title">{{ t.title }}</ToastTitle>
        <ToastDescription v-if="t.description">{{ t.description }}</ToastDescription>
      </div>
      <ToastClose />
    </Toast>
    <ToastViewport />
  </ToastProvider>
</template>
