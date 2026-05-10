<script setup lang="ts">
// TableCaption——<caption>，表格语义标题（屏幕阅读器读出"表格：xxx"），
// 默认 caption-bottom 让它显示在表格下方。无视觉装饰，纯辅助语义。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() =>
  cn('mt-4 text-sm text-muted-foreground', props.class),
)
</script>

<template>
  <caption :class="classes">
    <slot />
  </caption>
</template>
