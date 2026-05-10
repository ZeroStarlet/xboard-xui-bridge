// 全局 status 共享 composable（v0.7 视觉重构 — Bento Live Console）。
//
// 设计动机：
//
//   LiveStatusBar（顶部常驻）与 Dashboard 同时需要读 GET /api/status
//   的运行态数据；如果两个组件各自在 onMounted 调一次 + 各自定时刷新，
//   会产生：
//     1. 双倍请求（同一秒打两次 /api/status，浪费上游带宽）；
//     2. 视觉错位（两组件刷新时间不同步，同一个数据点上一个显示
//        "运行中"，另一个还是"加载中"）。
//
//   把 status 抽到 composable 单例（模块级 ref + 启动一次轮询）让所有
//   消费者共享同一份数据：useStatus() 返回的是 reactive ref，任意组件
//   修改它都看到最新值；轮询启动逻辑用 referenceCount 引用计数确保
//   只在有人订阅时跑、所有人取消订阅时停。
//
// 错误策略（与项目"严禁兜底/回退/降级"规范一致）：
//
//   - fetch 失败：不静默忽略、不假装成功——把错误透出到 lastError ref
//     让消费者按需展示（默认 LiveStatusBar 显示 destructive 心跳点）；
//   - 不重试、不退避、不切换备用 endpoint——单一正向路径。
//
// 不实现的功能：
//
//   - SSE / WebSocket：当前后端只暴露 GET /api/status，无推送通道。
//     如未来接入 Server-Sent Events，本 composable 内部切换数据源即可，
//     消费者代码不变。
//   - 增量补丁：每次都返回完整 Status，~200B JSON，无优化必要。
import { ref, onScopeDispose } from 'vue'
import { api, type Status } from '@/api/client'

/**
 * 默认轮询周期（毫秒）。
 *
 * 选 6s 而非 1s 的理由：
 *   - status API 后端从内存读，~ms 级，但浏览器 fetch 仍要 RTT；
 *     1s 周期会让弱网用户看到不连续的请求堆叠（前一个还没回，
 *     后一个又发）。
 *   - 桥接增删 / 引擎重载是低频运维操作，6s 已远超运维"看一眼数据
 *     等几秒"的耐心边界。
 */
const POLL_INTERVAL_MS = 6_000

// ============================================================
// 模块级单例（多个组件共享同一份 status ref + 同一个轮询循环）
// ============================================================

const status = ref<Status | null>(null)
const loading = ref<boolean>(false)
const lastError = ref<unknown>(null)

let pollTimer: ReturnType<typeof setInterval> | null = null
let refCount = 0

/**
 * 主动拉取一次 status。失败不抛——错误暂存到 lastError 让 UI 按需展示。
 *
 * 单一正向路径：成功则更新 status + 清空 lastError；失败仅设置
 * lastError 不清 status——保留上一刻成功值让运维看到"上次拉到的状态"，
 * 比一闪而过的 null 更有价值。
 */
async function refresh(): Promise<void> {
  loading.value = true
  try {
    const s = await api.getStatus()
    status.value = s
    lastError.value = null
  } catch (e) {
    lastError.value = e
  } finally {
    loading.value = false
  }
}

/**
 * 启动轮询（仅当尚未启动时）。引用计数 +1。
 *
 * setInterval 不在订阅期间触发首次执行——所以这里手动 refresh() 一次
 * 让首屏立刻有数据，避免运维盯着"加载中…"等满 6s。
 */
function startPolling(): void {
  refCount += 1
  if (pollTimer !== null) return
  void refresh()
  pollTimer = setInterval(() => {
    void refresh()
  }, POLL_INTERVAL_MS)
}

/**
 * 取消订阅。引用计数 -1，归零时停止轮询。
 *
 * 不直接清空 status：让用户切换路由（例如从 Dashboard 切到 Settings）
 * 期间，常驻 LiveStatusBar 仍能看到上一刻的数据；下次有组件挂载时
 * refresh() 会立刻覆盖。
 *
 * refCount clamp 到 0：HMR / Vue devtools 触发的组件 dispose 路径在
 * 极端情况下可能两次调用 onScopeDispose（例如 hot reload 期间组件被
 * 重建），让 refCount 减到负数后下次 startPolling 引用计数失衡——
 * Math.max(0, ...) 防止此类计数偏移（v0.7 第 2 轮 Codex nit 反馈 #10）。
 */
function stopPolling(): void {
  refCount = Math.max(0, refCount - 1)
  if (refCount > 0) return
  if (pollTimer !== null) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

/**
 * 共享 status composable。
 *
 * 调用方只在 setup 内调一次：
 *
 *   const { status, loading, lastError, refresh } = useStatus()
 *
 * 自动在组件 unmount 时取消订阅（onScopeDispose 是 setup-style 标准
 * 清理钩子，与 onUnmounted 等价但不要求组件必须挂在 DOM 树上）。
 *
 * 注意：refresh 是手动触发的接口（例如刷新按钮），与轮询互不冲突——
 * 用户点刷新会立刻拉一次，下个轮询周期到来时会再拉一次（间隔可能
 * < POLL_INTERVAL_MS），可接受。
 */
export function useStatus() {
  startPolling()
  onScopeDispose(stopPolling)
  return { status, loading, lastError, refresh }
}
