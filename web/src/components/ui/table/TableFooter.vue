<script setup lang="ts">
// TableFooter——<tfoot>，承载汇总行 / 页脚。视觉上有顶部 border 区分。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() =>
  cn('border-t bg-muted/50 font-medium [&>tr]:last:border-b-0', props.class),
)
</script>

<template>
  <tfoot :class="classes">
    <slot />
  </tfoot>
</template>
