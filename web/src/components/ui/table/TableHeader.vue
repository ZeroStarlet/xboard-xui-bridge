<script setup lang="ts">
// TableHeader——<thead>，让最后一行 tr 的 border-b-0 移除（避免与表体首行重复分割线）。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() => cn('[&_tr]:border-b', props.class))
</script>

<template>
  <thead :class="classes">
    <slot />
  </thead>
</template>
