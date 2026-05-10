<script setup lang="ts">
// 桥接管理（v0.5 视觉重构）。
//
// 视觉策略：
//   - 列表用 .data-table（与仪表盘一致）。
//   - 表单用"右侧抽屉"而非居中模态——抽屉滑入更现代化，且让用户能边看
//     列表边填表单（一些字段可能要参考已有桥接）。
//   - 删除按钮放进每行尾部的工具按钮组里，带图标。
//   - 空状态卡片化（与 Dashboard 一致），统一 UX 语言。
//
// 可访问性：抽屉打开时
//   a) 监听 Escape 键关闭——比"必须点击 X 按钮或背景"更顺手；
//   b) nextTick 后聚焦"名称"输入框——让用户立即开始输入而无需手动点；
//   c) 关闭后焦点归还到触发它的按钮（"新增桥接"或某行的"编辑"按钮）。
//   不实现完整 focus trap：v0.5 范围内 esc + autofocus 已覆盖 90% 场景。
import { ref, onMounted, computed, nextTick, watch } from 'vue'
import { api, ApiError, type Bridge } from '@/api/client'

const protocols = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'hysteria2']

const bridges = ref<Bridge[]>([])
const loading = ref(true)
const errMsg = ref('')
const okMsg = ref('')

// 表单状态：show=false 隐藏；editing=null 表示新增。
const show = ref(false)
const editing = ref<string | null>(null)
const form = ref({
  name: '',
  xboard_node_id: 0,
  xboard_node_type: '',
  xui_inbound_id: 0,
  protocol: 'vless',
  flow: '',
  enable: true,
})
const submitting = ref(false)

// 抽屉根容器 ref + 上次触发抽屉打开的元素（用于关闭时焦点归还）。
const drawerRoot = ref<HTMLElement | null>(null)
let lastTrigger: HTMLElement | null = null

// 监听 show：true 时 nextTick 聚焦名称输入框；false 时归还焦点。
watch(show, async (val) => {
  if (val) {
    await nextTick()
    // 优先让名称输入框拿焦点；编辑模式下名称只读，那就退化到 xboard_node_id。
    const target =
      drawerRoot.value?.querySelector<HTMLElement>('#bridge-name:not([disabled])') ??
      drawerRoot.value?.querySelector<HTMLElement>('#bridge-xboard-id') ??
      null
    target?.focus()
  } else if (lastTrigger) {
    // 关闭后归还焦点；用 setTimeout 让 transition 完成后再 focus，
    // 否则 transition 元素移除会立刻把焦点转到 body。
    const t = lastTrigger
    lastTrigger = null
    setTimeout(() => t.focus(), 50)
  }
})

function captureTrigger(e: Event) {
  // currentTarget 是绑定监听的元素本身；如果是 RouterLink 等组件被原生
  // event handler 截到 target 可能是子元素，这里用 currentTarget 拿稳定值。
  const t = e.currentTarget as HTMLElement | null
  if (t) lastTrigger = t
}

function onEscapeKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && show.value) {
    show.value = false
  }
}

const formTitle = computed(() => (editing.value ? `编辑 ${editing.value}` : '新增桥接'))

async function refresh() {
  loading.value = true
  errMsg.value = ''
  try {
    bridges.value = await api.listBridges()
  } catch (e) {
    errMsg.value = '加载桥接列表失败'
    console.warn(e)
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editing.value = null
  form.value = {
    name: '',
    xboard_node_id: 0,
    xboard_node_type: '',
    xui_inbound_id: 0,
    protocol: 'vless',
    flow: '',
    enable: true,
  }
  show.value = true
  okMsg.value = ''
  errMsg.value = ''
}

function openEdit(b: Bridge) {
  editing.value = b.name
  form.value = {
    name: b.name,
    xboard_node_id: b.xboard_node_id,
    xboard_node_type: b.xboard_node_type,
    xui_inbound_id: b.xui_inbound_id,
    protocol: b.protocol,
    flow: b.flow ?? '',
    enable: b.enable,
  }
  show.value = true
  okMsg.value = ''
  errMsg.value = ''
}

async function submit() {
  errMsg.value = ''
  okMsg.value = ''
  if (!form.value.name) {
    errMsg.value = '名称不可为空'
    return
  }
  if (form.value.xboard_node_id <= 0 || form.value.xui_inbound_id <= 0) {
    errMsg.value = 'Xboard 节点 ID 与 3x-ui inbound ID 必须为正整数'
    return
  }
  submitting.value = true
  try {
    if (editing.value) {
      await api.updateBridge(editing.value, form.value)
      okMsg.value = '已更新并触发引擎重载'
    } else {
      await api.createBridge(form.value)
      okMsg.value = '已创建并触发引擎重载'
    }
    show.value = false
    await refresh()
  } catch (e) {
    if (e instanceof ApiError) {
      errMsg.value = e.message
    } else {
      errMsg.value = '请求失败'
    }
  } finally {
    submitting.value = false
  }
}

async function remove(b: Bridge) {
  if (!confirm(`确认删除桥接 "${b.name}"？此操作不可撤销。`)) return
  try {
    await api.deleteBridge(b.name)
    okMsg.value = `已删除 ${b.name}`
    await refresh()
  } catch (e) {
    errMsg.value = e instanceof ApiError ? e.message : '删除失败'
  }
}

onMounted(refresh)
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7 flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-surface-900">桥接管理</h2>
        <p class="mt-1 text-sm text-surface-500">维护 Xboard 节点 ↔ 3x-ui inbound 的映射关系</p>
      </div>
      <button class="btn-primary" @click="(e) => { captureTrigger(e); openCreate() }">
        <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m7.5-7.5h-15" />
        </svg>
        新增桥接
      </button>
    </header>

    <!-- 提示横幅 -->
    <div v-if="errMsg" class="alert-error mb-5">
      <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
      </svg>
      <span>{{ errMsg }}</span>
    </div>
    <div v-if="okMsg" class="alert-success mb-5">
      <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
      <span>{{ okMsg }}</span>
    </div>

    <!-- 主表格 -->
    <section class="card">
      <div v-if="!loading && bridges.length === 0" class="rounded-xl border border-dashed border-surface-300 bg-surface-50/50 px-6 py-12 text-center">
        <svg class="mx-auto mb-3 h-10 w-10 text-surface-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round"
            d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244" />
        </svg>
        <p class="text-sm font-medium text-surface-700">暂无桥接</p>
        <p class="mt-1 text-xs text-surface-500">点击右上方"新增桥接"按钮添加第一个映射。</p>
      </div>

      <div v-else class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>名称</th>
              <th>协议</th>
              <th>Xboard 节点</th>
              <th>3x-ui inbound</th>
              <th>状态</th>
              <th class="text-right">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="b in bridges" :key="b.name">
              <td class="font-medium text-surface-900">{{ b.name }}</td>
              <td>
                <span class="font-mono text-[13px] text-surface-700">{{ b.protocol }}</span>
                <span v-if="b.flow" class="ml-1 text-xs text-surface-500">({{ b.flow }})</span>
              </td>
              <td>
                <span class="font-mono text-[13px]">{{ b.xboard_node_id }}</span>
                <span class="ml-1 text-xs text-surface-500">({{ b.xboard_node_type }})</span>
              </td>
              <td><span class="font-mono text-[13px]">{{ b.xui_inbound_id }}</span></td>
              <td>
                <span v-if="b.enable" class="pill-success">
                  <span class="pill-dot"></span>
                  <span>启用</span>
                </span>
                <span v-else class="pill-neutral">
                  <span class="pill-dot"></span>
                  <span>禁用</span>
                </span>
              </td>
              <td>
                <div class="flex justify-end gap-1">
                  <button
                    class="btn-icon"
                    :aria-label="`编辑 ${b.name}`"
                    @click="(e) => { captureTrigger(e); openEdit(b) }"
                  >
                    <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
                      <path stroke-linecap="round" stroke-linejoin="round"
                        d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0115.75 21H5.25A2.25 2.25 0 013 18.75V8.25A2.25 2.25 0 015.25 6H10" />
                    </svg>
                  </button>
                  <button
                    class="btn-icon hover:bg-rose-50 hover:text-rose-600"
                    :aria-label="`删除 ${b.name}`"
                    @click="remove(b)"
                  >
                    <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
                      <path stroke-linecap="round" stroke-linejoin="round"
                        d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
                    </svg>
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- 抽屉式表单 -->
    <transition
      enter-active-class="transition-opacity duration-200"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition-opacity duration-200"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="show"
        class="fixed inset-0 z-30 bg-surface-900/50 backdrop-blur-sm"
        @click.self="show = false"
        @keydown="onEscapeKey"
        ref="drawerRoot"
      >
        <transition
          enter-active-class="transition-transform duration-300 ease-out"
          enter-from-class="translate-x-full"
          enter-to-class="translate-x-0"
          leave-active-class="transition-transform duration-200 ease-in"
          leave-from-class="translate-x-0"
          leave-to-class="translate-x-full"
        >
          <!--
            role="region" + aria-label：抽屉 UX 模式不是真正的"模态对话框"——
            用户边看背景列表边填表单是有意为之的工作流（参考已有桥接的字段值）。
            v0.5 之前的草稿用过 role="dialog" + aria-modal="true"，但那要求
            完整 focus trap 才合规；既然功能上希望背景可见可交互，把语义降为
            region 才是真实表达。键盘体验仍由 Escape 关闭 + 进入聚焦首字段 +
            退出归还焦点三件套保证（详见 watch(show, ...) 与 onEscapeKey）。
          -->
          <aside
            v-if="show"
            class="fixed inset-y-0 right-0 flex w-full max-w-lg flex-col bg-white shadow-float"
            role="region"
            :aria-label="formTitle"
            tabindex="-1"
          >
            <header class="flex items-center justify-between border-b border-surface-200 px-6 py-5">
              <div>
                <h3 class="text-lg font-semibold text-surface-900">{{ formTitle }}</h3>
                <p class="mt-1 text-xs text-surface-500">
                  填写完成后将立即触发引擎热重载，无需重启进程。
                </p>
              </div>
              <button class="btn-icon" type="button" aria-label="关闭" @click="show = false">
                <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </header>

            <form @submit.prevent="submit" class="flex flex-1 flex-col overflow-hidden">
              <div class="flex-1 space-y-5 overflow-y-auto px-6 py-6">
                <div>
                  <label class="label" for="bridge-name">名称</label>
                  <input
                    id="bridge-name"
                    v-model="form.name"
                    class="input"
                    :disabled="!!editing"
                    placeholder="例如 hk-vless-01"
                  />
                  <p v-if="editing" class="help-text">名称作为主键，编辑时不可修改。</p>
                </div>
                <div class="grid grid-cols-2 gap-4">
                  <div>
                    <label class="label" for="bridge-xboard-id">Xboard 节点 ID</label>
                    <input
                      id="bridge-xboard-id"
                      v-model.number="form.xboard_node_id"
                      type="number"
                      min="1"
                      class="input input-mono"
                    />
                  </div>
                  <div>
                    <label class="label" for="bridge-xboard-type">Xboard 节点类型</label>
                    <input
                      id="bridge-xboard-type"
                      v-model="form.xboard_node_type"
                      class="input"
                      placeholder="留空 = 按 protocol 推断"
                    />
                  </div>
                </div>
                <div class="grid grid-cols-2 gap-4">
                  <div>
                    <label class="label" for="bridge-xui-id">3x-ui inbound ID</label>
                    <input
                      id="bridge-xui-id"
                      v-model.number="form.xui_inbound_id"
                      type="number"
                      min="1"
                      class="input input-mono"
                    />
                  </div>
                  <div>
                    <label class="label" for="bridge-protocol">协议</label>
                    <select id="bridge-protocol" v-model="form.protocol" class="input">
                      <option v-for="p in protocols" :key="p" :value="p">{{ p }}</option>
                    </select>
                  </div>
                </div>
                <div v-if="form.protocol === 'vless'">
                  <label class="label" for="bridge-flow">Flow（仅 VLESS）</label>
                  <input
                    id="bridge-flow"
                    v-model="form.flow"
                    class="input input-mono"
                    placeholder="例如 xtls-rprx-vision"
                  />
                </div>
                <div>
                  <label class="flex items-center gap-2.5 cursor-pointer" for="bridge-enable">
                    <input id="bridge-enable" v-model="form.enable" type="checkbox" class="cb" />
                    <span class="text-sm text-surface-700">启用此桥接</span>
                  </label>
                </div>

                <div v-if="errMsg" class="alert-error">
                  <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
                  </svg>
                  <span>{{ errMsg }}</span>
                </div>
              </div>

              <footer class="flex items-center justify-end gap-3 border-t border-surface-200 px-6 py-4 bg-surface-50/50">
                <button type="button" class="btn-secondary" @click="show = false">取消</button>
                <button type="submit" class="btn-primary" :disabled="submitting">
                  <svg
                    v-if="submitting"
                    class="h-4 w-4 animate-spin"
                    fill="none"
                    viewBox="0 0 24 24"
                    aria-hidden="true"
                  >
                    <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
                    <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
                  </svg>
                  {{ submitting ? '提交中…' : '保存' }}
                </button>
              </footer>
            </form>
          </aside>
        </transition>
      </div>
    </transition>
  </div>
</template>
