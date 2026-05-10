<script setup lang="ts">
// 命令面板（v0.7 视觉重构 — Bento Live Console — ⌘K / Ctrl+K）。
//
// 设计目标：
//
//   一处统一入口让运维"按 ⌘K → 输关键字 → 回车"完成所有跳转 / 切换：
//
//     - 导航：仪表盘 / 桥接 / 运行参数 / 账户
//     - 操作：刷新数据 / 退出登录
//     - 外观：亮 / 深 / 跟随系统
//     - 语言：简体中文 / English
//
//   交互参考 macOS Spotlight + Linear / Vercel / Notion 的 ⌘K：
//     - 全局键盘 ⌘K / Ctrl+K 唤起（main.ts 注册）
//     - 浮层居中 + 玻璃磁贴 + 大搜索框
//     - 上下方向键选中、Enter 执行、Esc 关闭
//     - 输入即时模糊过滤、按 group 分组展示
//
// 与 layout store 协作：
//
//   layout.cmdkOpen 单一来源——本组件用 v-if 控制挂载；任何打开 / 关闭
//   操作（⌘K 快捷键 / 状态条按钮 / 选项点击 / Esc）都改 store，无组件
//   私有的"打开状态"。
//
// 状态恢复：
//
//   每次打开重置 query='' + selectedIndex=0（上次输入不保留，运维下次
//   开 ⌘K 通常是新意图）。watch(open) 触发清理。
//
// 焦点管理（Codex 第 1 轮 major 反馈：v-if Teleport 不能依赖原生 dialog
// 焦点恢复，需要显式记录 / 还原 + Tab focus trap）：
//
//   - 打开前：把 document.activeElement 存入 previouslyFocused（通常
//     是 LiveStatusBar 的 ⌘K 按钮 / 状态条菜单 / 全局快捷键发起处的元素）；
//   - 打开后：nextTick 后 focus 输入框；
//   - 关闭后：把焦点显式还给 previouslyFocused.focus()——v-if 销毁
//     Teleport 内容时浏览器并不保证焦点回到合理位置，必须自己处理。
//
//   Tab focus trap：所有 cmdk-item button 都加 tabindex="-1"，让 Tab
//   键永远不离开 input 框（input 是 cmdk-shell 内唯一的 tab 目标）。
//   ↑↓ 方向键由 onKeydown 接管为 selectedIndex 切换。这种"单焦点 +
//   aria-activedescendant 虚拟选中"模式是 ARIA combobox/listbox 的
//   标准实现，比"逐元素 focus 切换"更稳健（避免 IME 输入打断 / 焦点
//   错位等边缘情况）。
//
// ARIA 语义（Codex 第 1 轮 major 反馈：补 listbox/option/combobox + ariaActiveDescendant）：
//
//   input  → role="combobox" + aria-expanded="true" + aria-controls=listboxId
//            + aria-autocomplete="list" + aria-activedescendant=当前选中项 id
//   ul     → role="listbox" + id=listboxId
//   button → role="option"  + id=optionId(flatIndex) + aria-selected=true/false
//
//   屏幕阅读器（NVDA / JAWS / VoiceOver）会朗读：
//     "搜索 / 跳转, 已聚焦, 编辑, 列表 12 项, 仪表盘 已选中"
//   方向键切换时它会主动朗读当前选中项 label。
//
// 不依赖 cmdk-vue 等库：12 条目 + 简单模糊匹配，自实现 ~200 行 vs
// 引入 ~30kb 依赖，不划算。
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import {
  LayoutDashboard,
  Network,
  SlidersHorizontal,
  UserRound,
  Sun,
  Moon,
  Monitor,
  Languages,
  RefreshCw,
  LogOut,
  Search,
  CornerDownLeft,
  ArrowUp,
  ArrowDown,
} from 'lucide-vue-next'
import { useLayoutStore } from '@/stores/layout'
import { useThemeStore, type ThemeMode } from '@/stores/theme'
import { useLocaleStore } from '@/stores/locale'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import { useStatus } from '@/composables/useStatus'
import { SUPPORTED_LOCALES, type SupportedLocale } from '@/i18n'

const { t } = useI18n()
const router = useRouter()
const layout = useLayoutStore()
const themeStore = useThemeStore()
const localeStore = useLocaleStore()
const authStore = useAuthStore()
const { toast } = useToast()
const { refresh: refreshStatus } = useStatus()

// 输入框 ref：打开时 focus。
const inputRef = ref<HTMLInputElement | null>(null)
const query = ref<string>('')
const selectedIndex = ref<number>(0)

// 打开前的活动元素——关闭时把焦点还给它（focus trap 配套）。
let previouslyFocused: HTMLElement | null = null

// listbox / option ARIA id 命名常量——保持稳定供 input
// aria-activedescendant 引用。
const LISTBOX_ID = 'cmdk-listbox'
function optionId(flatIndex: number): string {
  return `cmdk-option-${flatIndex}`
}

// 命令条目类型。一个条目 = {id 唯一键, 分组, label, 可选 hint / kbd, 图标, action}。
type CmdGroup = 'nav' | 'action' | 'theme' | 'locale'

interface CmdItem {
  id: string
  group: CmdGroup
  // labelKey：i18n key；用 key 而非已翻译字符串让切换语言时面板自动重新匹配。
  labelKey: string
  hintKey?: string
  // 图标用 lucide 组件，astatic import 避免运行时按字符串解析。
  icon: typeof LayoutDashboard
  // 快捷键 hint（kbd 显示用），与全局监听无关——仅视觉提示。
  shortcut?: string
  // 选中执行回调。
  action: () => void | Promise<void>
}

// 关闭面板的统一入口——执行后清状态。
function close(): void {
  layout.closeCmdK()
}

// 跳转后关闭面板的 helper。
function goto(path: string): void {
  router.push(path)
  close()
}

// 主题三态切换条目。本质上是把 ThemeToggle 的菜单项展平到 ⌘K 列表。
function setTheme(mode: ThemeMode): void {
  themeStore.setMode(mode)
  close()
}

function setLocale(loc: SupportedLocale): void {
  localeStore.setLocale(loc)
  close()
}

async function doLogout(): Promise<void> {
  close()
  await authStore.logout()
  toast({ title: t('common.logout'), variant: 'default', duration: 2000 })
  router.push('/login')
}

async function doRefresh(): Promise<void> {
  close()
  await refreshStatus()
  toast({ title: t('cmdk.actionRefreshed'), variant: 'success', duration: 1500 })
}

// 完整命令清单——computed 让 i18n 切换 / 主题切换时自动重排。
//
// 排序：先 nav（最常用）→ action（高频操作）→ theme（外观）→ locale（语言）。
// 这个顺序也是面板默认显示顺序。
const items = computed<CmdItem[]>(() => [
  // ---- nav ----
  { id: 'nav-dashboard', group: 'nav', labelKey: 'nav.dashboard', icon: LayoutDashboard, action: () => goto('/dashboard') },
  { id: 'nav-bridges',   group: 'nav', labelKey: 'nav.bridges',   icon: Network,         action: () => goto('/bridges')   },
  { id: 'nav-settings',  group: 'nav', labelKey: 'nav.settings',  icon: SlidersHorizontal, action: () => goto('/settings') },
  { id: 'nav-account',   group: 'nav', labelKey: 'nav.account',   icon: UserRound,       action: () => goto('/account')   },

  // ---- action ----
  { id: 'action-refresh', group: 'action', labelKey: 'cmdk.actionRefresh', icon: RefreshCw, action: doRefresh },
  { id: 'action-logout',  group: 'action', labelKey: 'common.logout',     icon: LogOut,    action: doLogout  },

  // ---- theme ----
  { id: 'theme-light',  group: 'theme', labelKey: 'theme.light',  icon: Sun,     action: () => setTheme('light')  },
  { id: 'theme-dark',   group: 'theme', labelKey: 'theme.dark',   icon: Moon,    action: () => setTheme('dark')   },
  { id: 'theme-system', group: 'theme', labelKey: 'theme.system', icon: Monitor, action: () => setTheme('system') },

  // ---- locale ----
  ...SUPPORTED_LOCALES.map<CmdItem>((loc) => ({
    id: `locale-${loc}`,
    group: 'locale',
    labelKey: `locale.${loc}`,
    icon: Languages,
    action: () => setLocale(loc),
  })),
])

/**
 * 模糊匹配：把 query 拆字符，依序在 label 内寻找——所有字符按顺序找到即匹配。
 *
 * 例子：query='dsh' 匹配 'Dashboard'（D…sh…）；query='设' 匹配'仪表盘'否？
 *      不——但匹配 '设置' / '运行参数'（"设" 字在中文 label 内）。
 *
 * 中文匹配：i18n 已把 zh-CN 文案统一为中文，运维输中文字符就能命中；
 *          英文 label 走英文匹配。混合输入（中英文夹杂）按原始字符序检查。
 *
 * 不实现：拼音首字母 / fuzzy.js 等高级匹配——~12 条目用不到。
 */
function matchesFuzzy(query: string, label: string): boolean {
  if (!query) return true
  const q = query.toLowerCase()
  const l = label.toLowerCase()
  let qi = 0
  for (let li = 0; li < l.length && qi < q.length; li += 1) {
    if (l[li] === q[qi]) qi += 1
  }
  return qi === q.length
}

// 过滤后的条目列表——按 query 模糊匹配 label 翻译值。
const filteredItems = computed<CmdItem[]>(() => {
  const q = query.value.trim()
  if (!q) return items.value
  return items.value.filter((it) => matchesFuzzy(q, t(it.labelKey)))
})

// 按 group 分组展示——保留 group 内原始顺序。
const groupedItems = computed<{ group: CmdGroup; items: CmdItem[] }[]>(() => {
  const order: CmdGroup[] = ['nav', 'action', 'theme', 'locale']
  return order
    .map((g) => ({ group: g, items: filteredItems.value.filter((it) => it.group === g) }))
    .filter((entry) => entry.items.length > 0)
})

// 把 group 的 i18n key 集中到一处（避免模板里写多个三元）。
const GROUP_LABEL_KEY: Record<CmdGroup, string> = {
  nav: 'cmdk.groupNav',
  action: 'cmdk.groupAction',
  theme: 'cmdk.groupTheme',
  locale: 'cmdk.groupLocale',
}

// =====================================================================
// 键盘交互 + 焦点管理
// =====================================================================

// query 变化导致条目数变化时把选中条复位到首条——避免上次选中的索引
// 落在空集或越界。watch 仅监听 filteredItems 数组的"身份"变化（filter
// 重新返回新数组时触发），不会因为 selectedIndex 自身变动 reentrant。
watch(filteredItems, () => {
  selectedIndex.value = 0
})

watch(
  () => layout.cmdkOpen,
  async (open) => {
    if (open) {
      // 打开前记录上次活动元素——关闭时归还焦点。document.activeElement
      // 在 ⌘K 触发时通常是 LiveStatusBar 的按钮 / body（快捷键全局触发
      // 时活动元素可能不在 cmdk 触发链上，此时回退到 body 也无害）。
      if (typeof document !== 'undefined') {
        const active = document.activeElement
        previouslyFocused = active instanceof HTMLElement ? active : null
      }
      query.value = ''
      selectedIndex.value = 0
      // 等下一帧 DOM 挂载完成再 focus——v-if=true 后 input 还未渲染。
      await nextTick()
      inputRef.value?.focus()
    } else {
      // 关闭时把焦点还给 previouslyFocused——浏览器在 v-if Teleport
      // 销毁内容时不保证焦点回到合理位置，必须显式调 .focus()。
      // 等一帧让 DOM 卸载完成再 focus，避免 focus 在已被销毁的输入框上。
      await nextTick()
      previouslyFocused?.focus()
      previouslyFocused = null
    }
  },
)

// 在 cmdk-shell 内监听 keydown 处理 ↑↓ Enter Esc + Tab focus trap。
function onKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') {
    e.preventDefault()
    close()
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIndex.value = Math.min(selectedIndex.value + 1, filteredItems.value.length - 1)
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIndex.value = Math.max(selectedIndex.value - 1, 0)
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    const item = filteredItems.value[selectedIndex.value]
    if (item) void item.action()
    return
  }
  // Tab focus trap——cmdk-item button 全部 tabindex="-1"，所以 input
  // 是 cmdk-shell 内唯一的 tab 目标。Tab / Shift+Tab 一律 preventDefault
  // 并把焦点强制留在 input 上——焦点永远不会逃逸到模态层之外。这也是
  // 标准 ARIA combobox/listbox 模式的约定（与原生 <select> 一致）。
  if (e.key === 'Tab') {
    e.preventDefault()
    inputRef.value?.focus()
    return
  }
}

// 全局快捷键 ⌘K / Ctrl+K——本组件挂载时注册到 window，卸载时移除。
//
// 在 App.vue 永驻挂载本组件 v-if=cmdkOpen 是错的——v-if 销毁会一起卸载
// 全局监听器。改用 v-show 又会让 query/index 状态在面板关闭后仍持有上次值。
//
// 折中方案：把全局快捷键监听放到 useLayoutStore() 调用方（main.ts 或
// App.vue setup）注册，与本组件生命周期解耦；本组件只管"打开时如何渲染"。
// → main.ts 不便加额外副作用，所以放到 App.vue 的 setup 内。
//
// 但 ⌘K 监听语义上属于 CommandPalette 自身——与其在 App.vue 里写监听，
// 不如让本组件用 onMounted/onUnmounted 控制。代价是 v-if=cmdkOpen 在
// 关闭时卸载，监听器随之失效；下次按 ⌘K 无响应。
//
// 解决：本组件改为永久挂载，内部用 v-if 控制覆盖层显示。组件常驻
// 但只有 layout.cmdkOpen=true 时渲染浮层 DOM——既保留全局快捷键，
// 又不浪费 DOM 节点。这是最终采用的方案。
function onGlobalKeydown(e: KeyboardEvent): void {
  // ⌘K (mac) / Ctrl+K (其他) 切换面板。
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault()
    layout.toggleCmdK()
  }
}

onMounted(() => {
  if (typeof window === 'undefined') return
  window.addEventListener('keydown', onGlobalKeydown)
})

onUnmounted(() => {
  if (typeof window === 'undefined') return
  window.removeEventListener('keydown', onGlobalKeydown)
})

// 把 group → flat index 映射出来供模板渲染时用 data-selected 标记当前项。
// flat index 与 filteredItems 顺序一致，所以这里通过逐组累加计算。
function flatIndexFor(group: CmdGroup, idxInGroup: number): number {
  let cum = 0
  for (const entry of groupedItems.value) {
    if (entry.group === group) return cum + idxInGroup
    cum += entry.items.length
  }
  return cum + idxInGroup
}
</script>

<template>
  <!--
    永久挂载策略：根节点用 v-if=cmdkOpen 控制浮层 DOM 创建/销毁。
    全局快捷键 onGlobalKeydown 通过 onMounted/onUnmounted 在 App.vue
    挂载本组件后即注册——只要本组件永驻在 App.vue 里，监听器就不会
    失效。打开 / 关闭 浮层只切换 v-if 内的 DOM，不卸载监听器。
  -->
  <Teleport to="body">
    <div
      v-if="layout.cmdkOpen"
      class="cmdk-overlay animate-fade-in"
      role="dialog"
      :aria-label="t('cmdk.aria')"
      aria-modal="true"
      @click.self="close"
      @keydown="onKeydown"
    >
      <div class="cmdk-shell animate-fade-in-up" @click.stop>
        <!-- 搜索输入区：左侧 Search 图标 absolute 定位，input 走 px-12 让出
             图标空间。Esc 关闭、↑↓ 选中、Enter 执行——keydown 事件冒泡到
             cmdk-overlay 已统一处理。 -->
        <div class="relative border-b">
          <Search
            class="absolute left-4 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <!--
            aria-controls / aria-activedescendant 仅在 listbox 真正渲染
            （filteredItems 非空）时绑定——空结果分支走的是 role="status"
            div，aria-controls 指向一个不存在的 id 会让屏幕阅读器
            把 input "悬挂引用"误报为损坏（v0.7 第 3 轮 Codex minor 反馈
            #1 修复）。Vue 把 undefined 序列化为属性缺省，无需手写 v-bind。
          -->
          <input
            ref="inputRef"
            v-model="query"
            type="text"
            class="cmdk-input"
            :placeholder="t('cmdk.placeholder')"
            :aria-label="t('cmdk.placeholder')"
            role="combobox"
            aria-expanded="true"
            aria-autocomplete="list"
            :aria-controls="filteredItems.length > 0 ? LISTBOX_ID : undefined"
            :aria-activedescendant="filteredItems.length > 0 ? optionId(selectedIndex) : undefined"
            autocomplete="off"
            spellcheck="false"
          />
        </div>

        <!--
          结果列表区 —— role="listbox" 让屏幕阅读器把它视为可选项集合；
          id 与 input aria-controls 配对。空态用单独 div（不是 listbox
          内的 status）避免 listbox 包含非 option 元素违反 ARIA 规范。
        -->
        <div
          v-if="filteredItems.length === 0"
          class="px-3 py-12 text-center text-sm text-muted-foreground"
          role="status"
          aria-live="polite"
        >
          {{ t('cmdk.empty') }}
        </div>
        <ul
          v-else
          :id="LISTBOX_ID"
          class="cmdk-list"
          role="listbox"
          :aria-label="t('cmdk.aria')"
        >
          <template v-for="entry in groupedItems" :key="entry.group">
            <li
              class="cmdk-group-label"
              role="presentation"
            >
              {{ t(GROUP_LABEL_KEY[entry.group]) }}
            </li>
            <li
              v-for="(item, i) in entry.items"
              :key="item.id"
              role="presentation"
            >
              <button
                :id="optionId(flatIndexFor(entry.group, i))"
                type="button"
                role="option"
                tabindex="-1"
                class="cmdk-item w-full text-left"
                :data-selected="flatIndexFor(entry.group, i) === selectedIndex"
                :aria-selected="flatIndexFor(entry.group, i) === selectedIndex"
                @click="item.action"
                @mouseenter="selectedIndex = flatIndexFor(entry.group, i)"
              >
                <component :is="item.icon" class="size-4 text-muted-foreground" aria-hidden="true" />
                <span class="flex-1">{{ t(item.labelKey) }}</span>
                <kbd
                  v-if="flatIndexFor(entry.group, i) === selectedIndex"
                  class="cmdk-shortcut"
                  aria-hidden="true"
                >
                  <CornerDownLeft class="size-3" aria-hidden="true" />
                </kbd>
              </button>
            </li>
          </template>
        </ul>

        <!-- 底栏快捷键 hint -->
        <div class="flex items-center justify-between border-t px-3 py-2 text-[11px] text-muted-foreground">
          <div class="flex items-center gap-2">
            <span class="inline-flex items-center gap-1">
              <kbd class="cmdk-shortcut"><ArrowUp class="size-3" aria-hidden="true" /></kbd>
              <kbd class="cmdk-shortcut"><ArrowDown class="size-3" aria-hidden="true" /></kbd>
              <span>{{ t('cmdk.hintNavigate') }}</span>
            </span>
            <span class="inline-flex items-center gap-1">
              <kbd class="cmdk-shortcut"><CornerDownLeft class="size-3" aria-hidden="true" /></kbd>
              <span>{{ t('cmdk.hintSelect') }}</span>
            </span>
            <span class="inline-flex items-center gap-1">
              <kbd class="cmdk-shortcut">esc</kbd>
              <span>{{ t('cmdk.hintClose') }}</span>
            </span>
          </div>
          <span>{{ t('cmdk.footer') }}</span>
        </div>
      </div>
    </div>
  </Teleport>
</template>
