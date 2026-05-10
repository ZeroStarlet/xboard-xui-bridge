// Pinia 主题（外观）状态。
//
// 设计目标：管理"亮 / 深 / 跟随系统"三态外观切换，并把选择持久化到
// localStorage、与 prefers-color-scheme 媒体查询联动。所有 UI 切换通过
// useThemeStore().setMode(...) 触发，单一来源。
//
// 三态语义：
//
//   - 'light'：强制亮色，忽略系统偏好
//   - 'dark'： 强制深色，忽略系统偏好
//   - 'system'：跟随 OS 设置（prefers-color-scheme），实时联动
//
// 实际生效的"是否深色"由 isDark 派生：
//
//   - mode='light'  → isDark=false
//   - mode='dark'   → isDark=true
//   - mode='system' → isDark=systemDark（matchMedia 监听结果）
//
// 应用机制：watch(isDark) immediate 切换 <html> 元素的 .dark class。
// Tailwind 在 darkMode='class' 模式下根据这个 class 决定 dark: 前缀
// utility 是否生效。所有 shadcn-vue 语义 token（CSS variable 在 :root /
// .dark 都有定义）随之自动重新计算，无需任何组件感知主题切换。
//
// localStorage 持久化：
//
//   - 写入：watch(mode) 把字符串 'light' / 'dark' / 'system' 写入 key。
//   - 读取：构造时调 loadStored() 同步加载；从未保存过则默认 'system'，
//     与"新装机用户应跟随系统"直觉一致。
//   - private 模式 / disabled storage 等异常一律 try/catch 静默忽略，
//     不向 UI 暴露——用户最差体验只是"切换不持久"。
//
// matchMedia 监听：
//
//   - 仅在 mode='system' 时生效；其它两态时虽然 systemDark 仍在更新，
//     但 isDark 派生不取它，对 UI 无影响。
//   - 监听器注册一次（store 初始化时），SPA 整个生命周期内不解绑——
//     onScopeDispose 在 setup-style store 内的清理时机是 Pinia 实例销毁，
//     SPA 场景下 == 浏览器关页面，此时已经不需要清理了。这是有意的：
//     避免引入未必有用的 cleanup 复杂度。
import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'

export type ThemeMode = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'xboard-bridge-theme'
const SUPPORTED_MODES: readonly ThemeMode[] = ['light', 'dark', 'system'] as const

export const useThemeStore = defineStore('theme', () => {
  // mode：用户选择的外观偏好（持久化）
  const mode = ref<ThemeMode>(loadStored())

  // systemDark：当前 OS 是否处于深色模式（仅在 mode='system' 时影响 UI）
  const systemDark = ref(detectSystemDark())

  // isDark：实际生效的"是否深色"
  const isDark = computed(() => {
    if (mode.value === 'dark') return true
    if (mode.value === 'light') return false
    return systemDark.value
  })

  // 应用到 <html>：immediate 让首次读取就生效（main.ts 在 mount 前调用 store
  // 即可让首屏渲染时就有正确的 .dark class，避免 flash of wrong theme）。
  watch(
    isDark,
    (dark) => {
      if (typeof document === 'undefined') return
      document.documentElement.classList.toggle('dark', dark)
      // 同步更新 color-scheme，让浏览器原生控件（滚动条、表单 default 风格）
      // 也跟随主题——避免深色面板里突然出现亮色滚动条。
      document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
    },
    { immediate: true },
  )

  // 持久化用户选择：mode 变化时写 localStorage。
  watch(mode, (m) => {
    if (typeof localStorage === 'undefined') return
    try {
      localStorage.setItem(STORAGE_KEY, m)
    } catch {
      // private mode / quota / disabled storage——静默忽略
    }
  })

  // matchMedia 监听 OS 主题切换。仅在 mode='system' 时影响 isDark；
  // 但监听器无条件注册，因为切换 mode 不应导致 listener 重新订阅
  // （订阅/取消的状态机过于复杂，得不偿失）。
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    const mql = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e: MediaQueryListEvent) => {
      systemDark.value = e.matches
    }
    // 老 Safari (<14) 不支持 addEventListener('change', ...)，需要 fallback
    // 到 addListener；现代浏览器两条 API 并行存在，以 addEventListener 为准。
    if (typeof mql.addEventListener === 'function') {
      mql.addEventListener('change', onChange)
    } else if (typeof (mql as unknown as { addListener?: (fn: (e: MediaQueryListEvent) => void) => void }).addListener === 'function') {
      ;(mql as unknown as { addListener: (fn: (e: MediaQueryListEvent) => void) => void }).addListener(onChange)
    }
  }

  /** 切换主题模式。三态值由调用方保证，越界值不写入。 */
  function setMode(m: ThemeMode) {
    if (!SUPPORTED_MODES.includes(m)) return
    mode.value = m
  }

  return { mode, isDark, setMode }
})

// =====================================================================
// 辅助函数（模块私有）
// =====================================================================

function loadStored(): ThemeMode {
  if (typeof localStorage === 'undefined') return 'system'
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw === 'light' || raw === 'dark' || raw === 'system') return raw
  } catch {
    // private mode / disabled storage——静默回退到默认值
  }
  return 'system'
}

function detectSystemDark(): boolean {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
    return false
  }
  return window.matchMedia('(prefers-color-scheme: dark)').matches
}
