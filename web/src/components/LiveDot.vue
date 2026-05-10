<script setup lang="ts">
// 心跳点（v0.7 视觉重构 — Bento Live Console 原子件）。
//
// 视觉策略：
//
//   一个圆形实心点 + 外层 absolute 元素做"雷达扩散光环"——周期 1.8s
//   scale 1→1.7 同步 opacity 0.7→0，模拟雷达扫描视觉。三种语义色：
//
//     - status='on'   品牌绿（emerald）+ 启用 pulse-ring 光环
//     - status='warn' 琥珀色（amber）  + 启用 pulse-ring 光环
//     - status='off'  灰色（surface）  + 静态外环（无光环动画）
//
// 与 .live-dot CSS 工具类的分工：
//
//   - 工具类负责"::before 实心点 + ::after 扩散光环 + animation"
//     的低层视觉细节；
//   - 本组件提供 Vue 层语义封装：props 控制色调与尺寸、span 容器
//     给布局工具留 slot——任何视图都能 <LiveDot status="on" /> 用，
//     无需感知 CSS 细节。
//
// 可访问性：
//
//   - 心跳点是装饰性视觉，文字 / aria 信息应由调用方在父级提供
//     （例如 "<LiveDot status="on"/> 引擎运行中"）。本组件根节点加
//     aria-hidden="true"，避免屏幕阅读器把心跳点本身朗读为"image"。
//
//   - prefers-reduced-motion 下 .live-dot::after 的 animation 已在
//     style.css 全局覆写为 none——本组件无需感知，依赖样式层契约。
import { computed } from 'vue'

type LiveStatus = 'on' | 'warn' | 'off'
type LiveSize = 'sm' | 'md' | 'lg'

// 各尺寸档对应的 Tailwind size utility——statically 写明而非
// 模板字符串拼接（`h-${...}` 会让 Tailwind 静态扫描丢字段）。
//
// 三档尺寸依据：
//   sm = h-1.5 w-1.5 (6px) —— 行内嵌入文字旁
//   md = h-2 w-2     (8px) —— 默认（与 .live-dot 默认一致）
//   lg = h-2.5 w-2.5 (10px) —— 大卡片 hero 区
const SIZE_CLASSES: Record<LiveSize, string> = {
  sm: 'h-1.5 w-1.5',
  md: 'h-2 w-2',
  lg: 'h-2.5 w-2.5',
}

const props = withDefaults(
  defineProps<{
    /** 状态语义：on=运行 / warn=异常 / off=停止。 */
    status?: LiveStatus
    /** 尺寸档：sm=6px / md=8px / lg=10px。 */
    size?: LiveSize
  }>(),
  { status: 'on', size: 'md' },
)

// 把 props 映射到 .live-dot-* 工具类（已在 style.css 注册，包括
// 配色 + pulse-ring 动画 + reduced-motion 覆写）。
const statusClass = computed(() => `live-dot-${props.status}`)
const sizeClass = computed(() => SIZE_CLASSES[props.size])
</script>

<template>
  <span
    class="live-dot"
    :class="[statusClass, sizeClass]"
    aria-hidden="true"
  />
</template>
