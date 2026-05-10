<script setup lang="ts">
// Alert 容器——页面级提示横幅。
//
// 用法（含 role 由调用方按语义选择）：
//
//   <!-- 成功提示 -->
//   <Alert variant="success" role="status">
//     <CheckCircle2 />
//     <AlertTitle>已保存</AlertTitle>
//     <AlertDescription>配置已写入并触发引擎热重载。</AlertDescription>
//   </Alert>
//
//   <!-- 失败错误 -->
//   <Alert variant="destructive" role="alert">
//     <XCircle />
//     <AlertTitle>登录失败</AlertTitle>
//     <AlertDescription>用户名或密码错误。</AlertDescription>
//   </Alert>
//
// 布局：has-[>svg] 条件 grid（CSS :has() 选择器）+ 子级 col-start-2 协同
//
//   - 无 svg 时：grid-cols-[0_1fr]（col 1 宽 0）+ 无 has-[>svg]:gap-x-3，
//     AlertTitle / AlertDescription 通过 col-start-2 跳到 col 2（占满全宽，
//     col 1 + gap 都是 0px，视觉无缩进）
//   - 有 svg 时：grid-cols-[auto_1fr]，svg 落 col 1（auto = 自身宽度），
//     gap-x-3 让 col 1 与 col 2 间留 12px，AlertTitle / Description col-start-2
//     落 col 2
//
//   父级条件 grid + 子级 col-start-2 双管齐下，缺一会让 grid auto-placement
//   把多个子节点错位排列（v0.6 初版踩过这个坑，详见 AlertTitle.vue 注释）。
//
//   这样 Alert 同时支持"纯文字"与"图标 + 文字"两种用法。
//   svg 用 [&>svg] 选择器选 Alert 直接子级（不会选到 Title 内嵌套的 svg），
//   自动应用 size-5 与微调 translate-y。
//
//   :has() 已在 Chrome 105+ / Firefox 121+ / Safari 15.4+ 支持，2024 年起
//   对运维后台用户群可用度极高，本批次依赖此特性。
import { computed, type HTMLAttributes } from 'vue'
import { cn } from '@/lib/utils'
// 从 ./variants 直接 import，避免组件 ⇄ barrel 自循环。
import { alertVariants, type AlertVariants } from './variants'

interface Props {
  variant?: AlertVariants['variant']
  class?: HTMLAttributes['class']
}

const props = defineProps<Props>()

const classes = computed(() =>
  cn(alertVariants({ variant: props.variant }), props.class),
)
</script>

<template>
  <div :class="classes">
    <slot />
  </div>
</template>
