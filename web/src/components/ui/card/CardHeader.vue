<script setup lang="ts">
// CardHeader：Card 顶部段，承载 CardTitle + CardDescription。
//
// 默认 padding：p-6（与 CardContent / CardFooter 等节奏一致），space-y-1.5
// 让 Title 与 Description 之间有自然行距而无需额外 mt-* class。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() => cn('flex flex-col space-y-1.5 p-6', props.class))
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
