<script setup lang="ts">
// 桥接管理页：列表 + 新增 / 编辑 / 删除。
//
// 表单与列表共享同一组件——简化实现：编辑时复用 form ref + 标记 editingName。
import { ref, onMounted, computed } from 'vue'
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
    <div class="flex items-center justify-between mb-6">
      <h2 class="text-2xl font-bold">桥接管理</h2>
      <button class="btn-primary" @click="openCreate">新增桥接</button>
    </div>

    <div v-if="errMsg" class="card mb-4 border-red-200 bg-red-50 text-red-700">{{ errMsg }}</div>
    <div v-if="okMsg" class="card mb-4 border-emerald-200 bg-emerald-50 text-emerald-700">{{ okMsg }}</div>

    <div class="card">
      <table class="w-full text-sm">
        <thead class="text-left text-gray-500 border-b">
          <tr>
            <th class="py-2 pr-4">名称</th>
            <th class="py-2 pr-4">协议</th>
            <th class="py-2 pr-4">Xboard 节点</th>
            <th class="py-2 pr-4">3x-ui inbound</th>
            <th class="py-2 pr-4">状态</th>
            <th class="py-2 text-right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!loading && bridges.length === 0">
            <td colspan="6" class="py-6 text-center text-gray-500">暂无桥接，点击右上方按钮添加。</td>
          </tr>
          <tr v-for="b in bridges" :key="b.name" class="border-b last:border-0">
            <td class="py-2 pr-4 font-medium">{{ b.name }}</td>
            <td class="py-2 pr-4">{{ b.protocol }}{{ b.flow ? ` (${b.flow})` : '' }}</td>
            <td class="py-2 pr-4">{{ b.xboard_node_id }} ({{ b.xboard_node_type }})</td>
            <td class="py-2 pr-4">{{ b.xui_inbound_id }}</td>
            <td class="py-2 pr-4">
              <span v-if="b.enable" class="px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">启用</span>
              <span v-else class="px-2 py-0.5 rounded bg-gray-100 text-gray-500 text-xs">禁用</span>
            </td>
            <td class="py-2 text-right space-x-2">
              <button class="btn-secondary text-xs" @click="openEdit(b)">编辑</button>
              <button class="btn-danger text-xs" @click="remove(b)">删除</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 表单模态层 -->
    <div v-if="show" class="fixed inset-0 bg-black/40 flex items-center justify-center p-4 z-10" role="dialog" aria-modal="true" aria-label="桥接表单">
      <div class="card w-full max-w-lg">
        <div class="flex items-center justify-between mb-4">
          <h3 class="text-lg font-bold">{{ formTitle }}</h3>
          <button type="button" class="text-gray-400 hover:text-gray-600 text-xl" aria-label="关闭" @click="show = false">×</button>
        </div>
        <form @submit.prevent="submit" class="space-y-4">
          <div>
            <label class="label">名称</label>
            <input v-model="form.name" class="input" :disabled="!!editing" />
            <p v-if="editing" class="text-xs text-gray-500 mt-1">名称作为主键，编辑时不可修改。</p>
          </div>
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label class="label">Xboard 节点 ID</label>
              <input v-model.number="form.xboard_node_id" type="number" min="1" class="input" />
            </div>
            <div>
              <label class="label">Xboard 节点类型</label>
              <input v-model="form.xboard_node_type" class="input" placeholder="留空自动按 protocol 推断" />
            </div>
          </div>
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label class="label">3x-ui inbound ID</label>
              <input v-model.number="form.xui_inbound_id" type="number" min="1" class="input" />
            </div>
            <div>
              <label class="label">协议</label>
              <select v-model="form.protocol" class="input">
                <option v-for="p in protocols" :key="p" :value="p">{{ p }}</option>
              </select>
            </div>
          </div>
          <div v-if="form.protocol === 'vless'">
            <label class="label">Flow（仅 VLESS）</label>
            <input v-model="form.flow" class="input" placeholder="例如 xtls-rprx-vision" />
          </div>
          <div>
            <label class="flex items-center gap-2">
              <input v-model="form.enable" type="checkbox" />
              <span class="text-sm">启用</span>
            </label>
          </div>
          <div v-if="errMsg" class="text-sm text-red-600">{{ errMsg }}</div>
          <div class="flex justify-end gap-2 pt-2">
            <button type="button" class="btn-secondary" @click="show = false">取消</button>
            <button type="submit" class="btn-primary" :disabled="submitting">
              {{ submitting ? '提交中…' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>
</template>
