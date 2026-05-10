<script setup lang="ts">
// DialogFooter——同 SheetFooter；小屏列反序，大屏行排列。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props { class?: HTMLAttributes['class'] }
const props = defineProps<Props>()
const classes = computed(() =>
  cn('flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2', props.class),
)
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
