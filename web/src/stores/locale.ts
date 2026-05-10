// Pinia 语言状态。
//
// 设计目标：管理当前显示语言（'zh-CN' / 'en-US'），与 vue-i18n 实例联动 +
// localStorage 持久化 + 浏览器自动检测（首次访问无 localStorage 时）。
//
// 与 vue-i18n 的分工：
//
//   - vue-i18n 的 i18n.global.locale 是当前生效语言的 ref；组件里 useI18n()
//     的 t() 函数从这个 ref 读取，反应式触发模板重渲染。
//   - 本 store 是它的"用户配置层"：维护 current（响应式）+ 持久化 +
//     初始化策略，watch(current) 把值写入 i18n.global.locale.value 完成同步。
//   - 不直接让组件改 i18n.global.locale.value：所有切换走 useLocaleStore().
//     setLocale(...)，确保持久化与 i18n 同步原子完成。
//
// 初始化优先级：
//
//   1. localStorage 中保存的有效值（用户上次显式选择）—— 最高优先级，
//      尊重用户偏好。
//   2. navigator.language 检测（首次访问，无 localStorage 记录）：
//      - 'zh-*' （包括 zh-CN / zh-TW / zh-HK 等）→ 'zh-CN'
//      - 其它任何值 → 'en-US'（默认回退）
//   3. 默认 'zh-CN'（理论上 navigator 不可用时的兜底；现代浏览器都有）
//
// 同步副作用（watch(current) immediate）：
//
//   1. 写入 i18n.global.locale.value（vue-i18n 实例切换语言）
//   2. 写入 localStorage（持久化）
//   3. 写入 <html lang="..."> 属性（无障碍 / 搜索引擎 / 浏览器 spell-check
//      用此决定语言行为；漏掉这步会让屏幕阅读器朗读发音错误）
//
// 不实现的功能：
//   - URL query 参数 ?lang=xxx 覆盖：现阶段无此需求，路由表保持简单
//   - cookie 同步到后端：i18n 完全前端态，不影响 API 行为
//   - 多语言资源动态懒加载：当前 2 个 locale + ~5KB JSON each，
//     全量打包成本可忽略；将来如果加 zh-TW / ja-JP / ko-KR 等再考虑
import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import { i18n, SUPPORTED_LOCALES, type SupportedLocale } from '@/i18n'

const STORAGE_KEY = 'xboard-bridge-locale'

export const useLocaleStore = defineStore('locale', () => {
  const current = ref<SupportedLocale>(loadStored())

  watch(
    current,
    (loc) => {
      // 1. vue-i18n 切换语言
      i18n.global.locale.value = loc
      // 2. localStorage 持久化
      if (typeof localStorage !== 'undefined') {
        try {
          localStorage.setItem(STORAGE_KEY, loc)
        } catch {
          // private mode / quota——静默忽略
        }
      }
      // 3. <html lang> 属性同步（屏幕阅读器、spell-check、SEO）
      if (typeof document !== 'undefined') {
        document.documentElement.setAttribute('lang', loc)
      }
    },
    { immediate: true },
  )

  /** 切换语言。超出支持列表的值被忽略，不会写入。 */
  function setLocale(loc: SupportedLocale) {
    if (!SUPPORTED_LOCALES.includes(loc)) return
    current.value = loc
  }

  return { current, setLocale }
})

// =====================================================================
// 辅助函数（模块私有）
// =====================================================================

function loadStored(): SupportedLocale {
  if (typeof localStorage !== 'undefined') {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (isSupported(raw)) return raw
    } catch {
      // private mode / disabled storage——继续走 navigator 检测
    }
  }
  return detectBrowserLocale()
}

function detectBrowserLocale(): SupportedLocale {
  if (typeof navigator === 'undefined') return 'zh-CN'
  const lang = (navigator.language || '').toLowerCase()
  if (lang.startsWith('zh')) return 'zh-CN'
  return 'en-US'
}

// 用类型守卫确保 raw 字符串收窄到 SupportedLocale 字面值类型。
function isSupported(s: string | null): s is SupportedLocale {
  return s !== null && (SUPPORTED_LOCALES as readonly string[]).includes(s)
}
