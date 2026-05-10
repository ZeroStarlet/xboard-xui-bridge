<script setup lang="ts">
// 折线图（v0.7 视觉重构 — Bento Live Console 原子件）。
//
// 设计目标：用最少代码画一条简洁的趋势折线 + 渐变填充——给 Dashboard
// 的"流量速率 / 用户增长 / 引擎运行" 等趋势卡当视觉锚点。不引入图表库
// （recharts / chartjs / d3 等），避免 ~80kb gzip 增量；当前需求仅是
// 单条折线 + 单色填充，纯 SVG path 即可。
//
// 数据契约：
//
//   - props.values: number[]——任意长度数据序列；空数组或单点会自动
//     回退到"占位虚线"以避免 NaN path（SVG 渲染会报错）。
//   - props.height / props.width 透传到 viewBox（SVG 自动响应式缩放）。
//   - 颜色由父级 color utility 决定（text-brand-500 / text-info-500
//     等），SVG path 走 currentColor 继承——一处定义多种主题。
//
// 视觉细节：
//
//   - 折线（stroke）走 stroke-linecap="round" + stroke-linejoin="round"
//     让转角不锋利，符合 Fluent 调性。
//   - 填充区域（fill）从折线下方延伸到 viewBox 底部，opacity=0.10
//     让填充淡出，配合折线产生"有体积"的视觉层次。
//   - viewBox 内做归一化——把 values 映射到 [padding, width-padding] x
//     [padding, height-padding]，padding=2 让线条不贴边。
//
// 性能：纯 computed，无 watch / 副作用。values 变化时 path d 自动
// 重新计算并 reactively 更新到 DOM，60fps 流畅。
import { computed } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 数据序列。空数组或单点会回退到占位虚线。 */
    values: number[]
    /** SVG viewBox 宽（默认 100，配合父容器 100% 缩放）。 */
    width?: number
    /** SVG viewBox 高（默认 32，扁平趋势线观感）。 */
    height?: number
  }>(),
  { width: 100, height: 32 },
)

// 内边距：让线条不贴 viewBox 边缘——边缘贴满会让 stroke 在容器
// 紧凑显示时被裁掉一半（stroke-width=2 在 0/0 端点会被裁掉 1px）。
const PADDING = 2

/**
 * 把 values 映射到 SVG viewBox 坐标空间。
 *
 * 数据归一化逻辑：
 *   - x 轴：等距分布，i ∈ [0, len-1] → [PADDING, width - PADDING]
 *   - y 轴：min/max 拉伸到 [PADDING, height - PADDING]，越大越靠上
 *     （SVG y 轴向下，所以要倒转）
 *   - 全等值序列（max=min）：所有点画在中线上，避免除零
 *
 * 返回 [{x, y}, ...] 点列，path d 由调用方拼接。
 */
const points = computed<{ x: number; y: number }[]>(() => {
  const vs = props.values
  // 空数组 / 单点：返回空，模板侧用 v-if 渲染占位
  if (!vs.length || vs.length < 2) return []
  const max = Math.max(...vs)
  const min = Math.min(...vs)
  const range = max - min
  const stepX = (props.width - 2 * PADDING) / (vs.length - 1)
  const usableY = props.height - 2 * PADDING
  const midY = props.height / 2
  return vs.map((v, i) => {
    const x = PADDING + i * stepX
    // 全等值序列：把所有点画在中线上
    const y =
      range === 0
        ? midY
        : props.height - PADDING - ((v - min) / range) * usableY
    return { x, y }
  })
})

/**
 * 折线 path——M x0 y0 L x1 y1 L x2 y2 ...
 *
 * 不用 quadratic / cubic 平滑曲线（Q / C）：v0.7 调性是"工程感运维
 * 中心"，棱角分明的折线比柔和曲线更"诚实"地反映数据采样间隔；想要
 * 平滑视觉的人应当先做数据降采样而不是 UI 端造曲线。
 */
const strokePath = computed<string>(() => {
  const pts = points.value
  if (!pts.length) return ''
  return pts.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x} ${p.y}`).join(' ')
})

/**
 * 填充 path——折线 + 右下角 + 左下角，闭合成多边形。
 *
 * 拼接顺序：
 *   M 第一点 → L 沿折线各点 → L 右下角 → L 左下角 → Z（闭合）
 *
 * Z 让 path 自动连回起点，比手写 `L pts[0].x pts[0].y` 更稳——
 * 浏览器不会因为浮点误差出现"几乎闭合但差 0.001px"的视觉裂缝。
 */
const fillPath = computed<string>(() => {
  const pts = points.value
  if (!pts.length) return ''
  const stroke = pts.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x} ${p.y}`).join(' ')
  const last = pts[pts.length - 1]
  return `${stroke} L ${last.x} ${props.height} L ${pts[0].x} ${props.height} Z`
})

/**
 * viewBox 字符串拼接——单独 computed 避免模板里写表达式。
 */
const viewBox = computed(() => `0 0 ${props.width} ${props.height}`)
</script>

<template>
  <!--
    有数据：渲染填充 path（fill 区域）+ 折线 path（stroke）。
    无数据：渲染中线虚线占位——告知"当前序列为空"而不是空白卡。
  -->
  <svg
    class="sparkline-svg"
    :viewBox="viewBox"
    preserveAspectRatio="none"
    aria-hidden="true"
    role="presentation"
  >
    <template v-if="points.length >= 2">
      <path class="sparkline-fill" :d="fillPath" />
      <path class="sparkline-stroke" :d="strokePath" />
    </template>
    <line
      v-else
      :x1="PADDING"
      :y1="height / 2"
      :x2="width - PADDING"
      :y2="height / 2"
      stroke="currentColor"
      stroke-width="1"
      stroke-dasharray="3 3"
      opacity="0.40"
    />
  </svg>
</template>
