<script setup lang="ts">
// AlertDescription：Alert 内的描述段。
//
// text-sm 让描述比标题略小；[&_p]:leading-relaxed 给嵌套段落留出阅读空间
// （多行描述时呼吸感更强）。
//
// col-start-2 与 AlertTitle 同理——父级 Alert 的 has-[>svg] 条件 grid +
// 子级 col-start-2 协同，确保无 svg 时跳过 0 宽 col 1（无视觉缩进），
// 有 svg 时落在 col 2（svg 右侧）。详见 AlertTitle.vue 的注释解释。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'

interface Props {
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn(
    'col-start-2 grid justify-items-start gap-1 text-sm [&_p]:leading-relaxed',
    props.class,
  ),
)
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
