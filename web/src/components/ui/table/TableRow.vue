<script setup lang="ts">
// TableRow——<tr>，hover 高亮 + 选中态。
//
// data-[state=selected]:bg-muted 让父级通过 :data-state="..." 标记选中行
// （例如多选场景），无选中需求时无副作用。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() =>
  cn(
    'border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted',
    props.class,
  ),
)
</script>

<template>
  <tr :class="classes">
    <slot />
  </tr>
</template>
