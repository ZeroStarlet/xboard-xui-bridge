<script setup lang="ts">
// TableBody——<tbody>，最后一行 tr 的 border-b-0 移除（避免与卡片底缘冲突）。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() => cn('[&_tr:last-child]:border-0', props.class))
</script>

<template>
  <tbody :class="classes">
    <slot />
  </tbody>
</template>
