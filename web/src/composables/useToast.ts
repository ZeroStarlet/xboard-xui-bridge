// useToast composable——全局 toast 队列管理。
//
// 设计思想（与 shadcn-vue 官方一致）：
//
//   - 模块级 reactive 状态：单例 toasts 数组，所有组件 import 同一个；
//     不放 Pinia 是因为 toast 队列不需要持久化、不需要跨标签页同步、
//     不需要 devtools 调试，最简单的全局可响应数据源就够。
//
//   - reka-ui Toast 已处理 open / 自动消失 / 动画——本 composable 只负责
//     "向队列追加、限流、id 分配、对外提供 toast() / dismiss() 方法"。
//
//   - 限流：TOAST_LIMIT=5 防止失控的连续错误把屏幕铺满。超过限制时丢弃
//     最旧的（队首）。
//
//   - 自动消失：默认 5 秒由 reka-ui ToastRoot 的 duration prop 处理；
//     调用方传 duration: 0 则永不自动消失（仅限严重错误，需要用户主动关闭）。
//
// 使用：
//
//   const { toast } = useToast()
//   toast({ title: '已保存', description: '配置生效', variant: 'success' })
//   toast({ title: '保存失败', description: e.message, variant: 'destructive', duration: 0 })
import { ref, computed, type ComputedRef } from 'vue'

/** 单个 toast 入口数据。 */
export interface ToastOptions {
  /** 显式 id；不传时自增。同 id 的二次调用会替换前一条而非追加。 */
  id?: string
  title?: string
  description?: string
  variant?: 'default' | 'destructive' | 'success' | 'warning' | 'info'
  /**
   * 自动消失时长（ms）。0 = 永不自动消失（用户必须手动关闭）。
   * 默认 5000 = 5 秒，对运维"看到了" + "不刷屏"的平衡值。
   */
  duration?: number
}

/** 队列中实际持有的 toast 状态。 */
export interface ToastEntry extends Required<Omit<ToastOptions, 'id'>> {
  id: string
  open: boolean
}

const TOAST_LIMIT = 5
const DEFAULT_DURATION = 5000

// 退场动画时长——与 reka-ui ToastRoot 默认 close 动画时长一致（ToastRoot
// 在 data-[state=closed] 时播放 slide-out + fade-out，约 200-300ms）。
// dismiss 必须延后这么久才从数组移除 entry，否则 v-for 立刻销毁组件实例，
// 退场动画来不及播——用户看到 toast"瞬间消失"而非"滑出"。
const ANIMATION_DURATION_MS = 300

let counter = 0
const genId = () => `t-${++counter}`

const toasts = ref<ToastEntry[]>([])

function add(options: ToastOptions): { id: string; dismiss: () => void } {
  const id = options.id ?? genId()
  const entry: ToastEntry = {
    id,
    title: options.title ?? '',
    description: options.description ?? '',
    variant: options.variant ?? 'default',
    duration: options.duration ?? DEFAULT_DURATION,
    open: true,
  }

  // 同 id 替换：让"重复触发同一类型 toast"不会刷屏（例如"加载失败"在 retry
  // 期间反复 toast 时，固定 id 让最后一次替换前面的，而不是堆 5 条相同 toast）。
  const existingIndex = toasts.value.findIndex((t) => t.id === id)
  if (existingIndex >= 0) {
    toasts.value[existingIndex] = entry
  } else {
    toasts.value.push(entry)
    // 限流：超过 TOAST_LIMIT 时丢弃最旧的（队首）。
    if (toasts.value.length > TOAST_LIMIT) {
      toasts.value.shift()
    }
  }

  return { id, dismiss: () => dismiss(id) }
}

/**
 * 关闭 toast。
 *
 * 两阶段：
 *   1. 立即把 entry.open 置为 false——reka-ui ToastRoot 监听到 :open=false
 *      会切到 data-[state=closed]，触发退场动画
 *   2. 延后 ANIMATION_DURATION_MS 后从数组移除 entry——让动画播完再卸载
 *      组件实例，避免"瞬间消失"的视觉跳变
 *
 * @param id 不传 = 一键清空（页面切换 / 登出时常用）。
 */
function dismiss(id?: string) {
  if (id === undefined) {
    // 一键清空：先全部标 open=false 触发退场动画，再延后清空数组
    toasts.value.forEach((t) => {
      t.open = false
    })
    setTimeout(() => {
      toasts.value = []
    }, ANIMATION_DURATION_MS)
    return
  }
  const entry = toasts.value.find((t) => t.id === id)
  if (!entry) return
  entry.open = false
  setTimeout(() => {
    toasts.value = toasts.value.filter((t) => t.id !== id)
  }, ANIMATION_DURATION_MS)
}

/**
 * 在组件中获取 toast 控制能力。
 *
 * 返回值：
 *   - toasts: 响应式只读队列，Toaster.vue 用它渲染所有当前 toast
 *   - toast: 添加新 toast（返回 { id, dismiss }）
 *   - dismiss: 移除单条或一键清空
 *
 * 注意：toasts 是 ComputedRef 而非直接 ref——避免业务代码意外修改队列；
 * 增删一律走 toast() / dismiss() 方法保证一致行为。
 */
export function useToast(): {
  toasts: ComputedRef<ToastEntry[]>
  toast: (options: ToastOptions) => { id: string; dismiss: () => void }
  dismiss: (id?: string) => void
} {
  return {
    toasts: computed(() => toasts.value),
    toast: add,
    dismiss,
  }
}
