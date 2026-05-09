<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { api, type Status, type Bridge } from '@/api/client'

const status = ref<Status | null>(null)
const bridges = ref<Bridge[]>([])
const loading = ref(true)
const errMsg = ref('')

async function refresh() {
  loading.value = true
  errMsg.value = ''
  try {
    const [s, bs] = await Promise.all([api.getStatus(), api.listBridges()])
    status.value = s
    bridges.value = bs
  } catch (e) {
    errMsg.value = '加载状态失败'
    console.warn(e)
  } finally {
    loading.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h2 class="text-2xl font-bold">仪表盘</h2>
      <button class="btn-secondary" @click="refresh" :disabled="loading">{{ loading ? '加载中…' : '刷新' }}</button>
    </div>

    <div v-if="errMsg" class="card mb-4 border-red-200 bg-red-50 text-red-700">{{ errMsg }}</div>

    <div v-if="status" class="grid grid-cols-1 md:grid-cols-4 gap-4 mb-6">
      <div class="card">
        <p class="text-sm text-gray-500">引擎状态</p>
        <p class="text-2xl font-bold mt-1" :class="status.running ? 'text-emerald-600' : 'text-red-600'">
          {{ status.running ? '运行中' : '已停止' }}
        </p>
      </div>
      <div class="card">
        <p class="text-sm text-gray-500">启用桥接</p>
        <p class="text-2xl font-bold mt-1">{{ status.enabled_bridge_count }} / {{ status.total_bridge_count }}</p>
      </div>
      <div class="card">
        <p class="text-sm text-gray-500">凭据完整性</p>
        <p class="text-2xl font-bold mt-1" :class="status.creds_complete ? 'text-emerald-600' : 'text-amber-600'">
          {{ status.creds_complete ? '已配置' : '未完整' }}
        </p>
      </div>
      <div class="card">
        <p class="text-sm text-gray-500">监听地址</p>
        <p class="text-base font-mono mt-2 break-all">{{ status.listen_addr }}</p>
      </div>
    </div>

    <div class="card">
      <h3 class="text-lg font-semibold mb-3">桥接概览</h3>
      <div v-if="bridges.length === 0" class="text-sm text-gray-500">
        尚未配置任何桥接，请前往 <RouterLink to="/bridges" class="text-brand underline">桥接管理</RouterLink> 添加。
      </div>
      <table v-else class="w-full text-sm">
        <thead class="text-left text-gray-500 border-b">
          <tr>
            <th class="py-2 pr-4">名称</th>
            <th class="py-2 pr-4">协议</th>
            <th class="py-2 pr-4">Xboard 节点</th>
            <th class="py-2 pr-4">3x-ui inbound</th>
            <th class="py-2">状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in bridges" :key="b.name" class="border-b last:border-0">
            <td class="py-2 pr-4 font-medium">{{ b.name }}</td>
            <td class="py-2 pr-4">{{ b.protocol }}</td>
            <td class="py-2 pr-4">{{ b.xboard_node_id }} ({{ b.xboard_node_type }})</td>
            <td class="py-2 pr-4">{{ b.xui_inbound_id }}</td>
            <td class="py-2">
              <span v-if="b.enable" class="px-2 py-0.5 rounded bg-emerald-100 text-emerald-700 text-xs">启用</span>
              <span v-else class="px-2 py-0.5 rounded bg-gray-100 text-gray-500 text-xs">禁用</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
