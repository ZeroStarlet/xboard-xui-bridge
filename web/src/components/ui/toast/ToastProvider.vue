<script setup lang="ts">
// ToastProvider——透传 reka-ui ToastProvider，提供整个 toast 子树需要的 context
// （swipeDirection / duration 默认值等）。Toaster.vue 内单次实例化即可。
import { ToastProvider, type ToastProviderProps } from 'reka-ui'

defineProps<ToastProviderProps>()
</script>

<template>
  <ToastProvider v-bind="$props">
    <slot />
  </ToastProvider>
</template>
