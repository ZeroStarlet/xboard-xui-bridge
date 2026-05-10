// Pinia 布局状态（v0.7 视觉重构 — Bento Live Console）。
//
// 设计目标：管理"主面板 shell"层级的两个跨组件状态——
//
//   1. navRail（侧栏折叠态）：true=64px rail / false=240px expanded。
//      LiveStatusBar 的折叠按钮 + AppNav 自身的悬停切换都改它，
//      Dashboard / Bridges / Settings 等内容区无需感知。
//
//   2. cmdkOpen（⌘K 命令面板开关）：true=打开浮层 / false=关闭。
//      LiveStatusBar 的 ⌘K 按钮、全局键盘监听（main.ts 注册的
//      window.keydown）、CommandPalette 自身的 Esc 关闭，都改它。
//
// 持久化：仅 navRail 持久化到 localStorage（用户偏好）；cmdkOpen 是
// 瞬时交互态不持久化——刷新后命令面板默认关闭即可。
//
// 默认值：navRail=false（展开）——首次访问应展示完整导航文字让运维
// 知道"这里有哪些区"，老用户切换到 rail 后选择被记住。
//
// 无 reduce-motion 适配：折叠动画走 CSS transition，CSS 层 @media
// (prefers-reduced-motion: reduce) 已统一处理，本 store 不需感知。
import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

const STORAGE_KEY_RAIL = 'xboard-bridge-nav-rail'

export const useLayoutStore = defineStore('layout', () => {
  const navRail = ref<boolean>(loadStoredRail())
  const cmdkOpen = ref<boolean>(false)

  // navRail 持久化：写入 localStorage。private mode / quota 静默忽略。
  watch(navRail, (collapsed) => {
    if (typeof localStorage === 'undefined') return
    try {
      localStorage.setItem(STORAGE_KEY_RAIL, collapsed ? '1' : '0')
    } catch {
      // private mode / quota——静默忽略
    }
  })

  /** 切换侧栏折叠态。 */
  function toggleNavRail(): void {
    navRail.value = !navRail.value
  }

  /** 显式设置侧栏折叠态——LiveStatusBar 的"展开"操作用。 */
  function setNavRail(collapsed: boolean): void {
    navRail.value = collapsed
  }

  /** 打开命令面板。 */
  function openCmdK(): void {
    cmdkOpen.value = true
  }

  /** 关闭命令面板。Esc / 选中条目后由 CommandPalette 触发。 */
  function closeCmdK(): void {
    cmdkOpen.value = false
  }

  /** 切换命令面板。⌘K 全局快捷键用——已开则关，已关则开。 */
  function toggleCmdK(): void {
    cmdkOpen.value = !cmdkOpen.value
  }

  return {
    navRail,
    cmdkOpen,
    toggleNavRail,
    setNavRail,
    openCmdK,
    closeCmdK,
    toggleCmdK,
  }
})

// =====================================================================
// 辅助函数（模块私有）
// =====================================================================

function loadStoredRail(): boolean {
  if (typeof localStorage === 'undefined') return false
  try {
    const raw = localStorage.getItem(STORAGE_KEY_RAIL)
    if (raw === '1') return true
    if (raw === '0') return false
  } catch {
    // private mode / disabled storage——继续返回默认值
  }
  return false
}
