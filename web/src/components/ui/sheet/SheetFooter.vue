<script setup lang="ts">
// SheetFooter——抽屉底部，承载操作按钮组。
//
// flex-col-reverse 在小屏让"主操作"按钮永远在最上（手指最容易触达），
// sm:flex-row 大屏改为水平排列从右到左。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

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
