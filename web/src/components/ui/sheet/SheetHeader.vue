<script setup lang="ts">
// SheetHeader——抽屉顶部段，承载 Title + Description。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn('flex flex-col space-y-2 text-center sm:text-left', props.class),
)
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
