// vue-i18n 实例化（v0.6 国际化骨架）。
//
// 设计要点：
//
//   1. legacy=false：采用 Composition API 模式，与项目其它 Pinia store + Vue 3
//      script setup 风格一致。views 里通过 `const { t } = useI18n()` 取翻译函数。
//
//   2. messages 直接 import JSON：vite 把 JSON 作为模块整体打入 bundle——
//      模板里的 t('xxx.yyy') 是运行时字符串查表而非静态 import，所以无法
//      tree-shake 单个 key（也不需要：未引用的 key 仍然属于 messages 对象，
//      引用的 key 必然要查到）。两套 message 共 ~10KB gzip 后 ~3KB，远小于
//      一次额外 HTTP 往返的成本。后续若新增 zh-TW/ja-JP 等语言变多再考虑
//      动态 import 按需加载。
//
//   3. fallbackLocale='en-US'：当 zh-CN 缺 key（开发期遗漏未翻译）时回退到
//      英文版而非显示 raw key string，对终端用户更友好。
//
//   4. SUPPORTED_LOCALES + SupportedLocale：把"支持哪些语言"作为单一来源
//      固化到类型系统，stores/locale.ts、LanguageSwitcher 组件、URL 参数
//      解析等所有处理 locale 的代码都从这里取，避免硬编码字符串散落。
//
//   5. 默认 locale='zh-CN'：与 Stage 0 项目实情一致——v0.5 之前所有
//      hard-coded 文案都是简中，运维基本盘也是中文用户。stores/locale.ts
//      会在初始化时根据 localStorage 持久化值或 navigator.language 覆写
//      此默认值，所以这里只是个安全 fallback。
//
//   6. 不在本文件做 Pinia 集成：i18n 实例本身是 Vue plugin，main.ts 通过
//      app.use(i18n) 装配；切换语言由 stores/locale.ts 直接写入
//      i18n.global.locale.value（vue-i18n 9+ Composition 模式的标准做法）。
import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN.json'
import enUS from './locales/en-US.json'

/**
 * 项目支持的 locale 列表。
 *
 * 每加一个新语言要做的事：
 *   1. 在 `src/i18n/locales/` 新增 `xx-YY.json`，结构与 zh-CN 完全对齐；
 *   2. 把代码加到下面的 `SUPPORTED_LOCALES` 数组；
 *   3. 在 messages 字段里 import + 注册；
 *   4. （可选）在 LanguageSwitcher 组件里加显示名映射。
 */
export const SUPPORTED_LOCALES = ['zh-CN', 'en-US'] as const

/** 字面值类型，凡是接受 locale 入参的函数签名都用此类型。 */
export type SupportedLocale = (typeof SUPPORTED_LOCALES)[number]

/**
 * 共享 i18n 实例。
 *
 * 所有视图通过 `useI18n()`（Composition API）取 t / d / n 函数；
 * 所有 store / 非组件代码通过 `i18n.global.t(...)` 取翻译。两条路径
 * 共享同一个 messages 树与同一个 locale ref，切换语言即时生效。
 */
export const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  fallbackLocale: 'en-US',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS,
  },
  // missingWarn / fallbackWarn：开发期开启可帮助及时发现未翻译 key；
  // 生产期由 vite 的 import.meta.env.PROD 自动关闭，避免 console 噪声。
  missingWarn: !import.meta.env.PROD,
  fallbackWarn: !import.meta.env.PROD,
})
