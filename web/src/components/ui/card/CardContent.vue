<script setup lang="ts">
// CardContent：Card 主体内容区。
//
// 默认 padding：p-6 pt-0——pt-0 是因为 CardHeader 上方已经 p-6，两段之间
// 不再叠加上 padding 才能让"标题 + 内容"在视觉上紧凑。如果调用方不用
// CardHeader（直接 Card > CardContent），可以 :class="'pt-6'" 补回。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() => cn('p-6 pt-0', props.class))
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
