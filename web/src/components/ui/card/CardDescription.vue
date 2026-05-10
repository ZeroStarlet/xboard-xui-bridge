<script setup lang="ts">
// CardDescription：CardTitle 下方的副标题文字。
// 渲染为 <p>，muted-foreground 颜色 + 较小字号。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() => cn('text-sm text-muted-foreground', props.class))
</script>

<template>
  <p :class="classes">
    <slot />
  </p>
</template>
