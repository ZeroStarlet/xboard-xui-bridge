<script setup lang="ts">
// CardFooter：Card 底部操作区，承载按钮组等。
//
// 默认 padding：p-6 pt-0（与 CardContent 同理，与 Header 不重复 padding）；
// flex 让多个按钮水平排列。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() => cn('flex items-center p-6 pt-0', props.class))
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
