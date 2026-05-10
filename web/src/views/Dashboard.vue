<script setup lang="ts">
// 仪表盘（v0.5 视觉重构）。
//
// 信息架构：
//   - 顶部页面标题 + 操作区（刷新按钮）
//   - KPI 区：4 张大数据卡（引擎状态 / 桥接 / 凭据 / 监听）—— 每张卡片有
//     色彩化指标 + 图标徽标，运维一眼能看出系统状态。
//   - 桥接概览表：复用 .data-table 视觉。
//
// 视觉细节：
//   - KPI 卡的图标徽章使用与状态匹配的色调（运行中=绿，未完整=琥珀，
//     等等）——色彩比纯文字更快传达"是否健康"。
//   - 加载态：用 skeleton 占位（KPI 卡灰底 + shimmer 动画），比"加载中…"
//     文字感觉更专业。
import { ref, onMounted, computed } from 'vue'
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

// 派生：根据 status 字段计算各 KPI 的视觉状态（色调）。
const engineState = computed(() => {
  if (!status.value) return { text: '—', tone: 'neutral' }
  return status.value.running
    ? { text: '运行中', tone: 'success' }
    : { text: '已停止', tone: 'danger' }
})
const credsState = computed(() => {
  if (!status.value) return { text: '—', tone: 'neutral' }
  return status.value.creds_complete
    ? { text: '已配置', tone: 'success' }
    : { text: '未完整', tone: 'warning' }
})

onMounted(refresh)
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7 flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-surface-900">仪表盘</h2>
        <p class="mt-1 text-sm text-surface-500">实时查看引擎与桥接运行状态</p>
      </div>
      <button class="btn-secondary" @click="refresh" :disabled="loading">
        <svg
          class="h-4 w-4"
          :class="{ 'animate-spin': loading }"
          fill="none"
          viewBox="0 0 24 24"
          stroke="currentColor"
          stroke-width="1.75"
          aria-hidden="true"
        >
          <path stroke-linecap="round" stroke-linejoin="round"
            d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" />
        </svg>
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </header>

    <!-- 错误提示 -->
    <div v-if="errMsg" class="alert-error mb-6">
      <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
        <path stroke-linecap="round" stroke-linejoin="round"
          d="M12 9v3.75m9-.75a9 9 0 11-18 0 9 9 0 0118 0zm-9 3.75h.008v.008H12v-.008z" />
      </svg>
      <span>{{ errMsg }}</span>
    </div>

    <!-- KPI 网格 -->
    <section class="mb-7 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <!-- 引擎状态卡 -->
      <div class="kpi-card">
        <div class="flex items-start justify-between">
          <span class="kpi-label">引擎状态</span>
          <span
            class="flex h-8 w-8 items-center justify-center rounded-lg"
            :class="status?.running ? 'bg-brand-50 text-brand-600' : 'bg-rose-50 text-rose-600'"
          >
            <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 3l14 9-14 9V3z" />
            </svg>
          </span>
        </div>
        <p
          class="kpi-value"
          :class="{
            'text-brand-600': engineState.tone === 'success',
            'text-rose-600': engineState.tone === 'danger',
            'text-surface-400': engineState.tone === 'neutral',
          }"
        >
          {{ engineState.text }}
        </p>
        <span
          class="self-start"
          :class="{
            'pill-success': engineState.tone === 'success',
            'pill-danger': engineState.tone === 'danger',
            'pill-neutral': engineState.tone === 'neutral',
          }"
        >
          <span class="pill-dot"></span>
          <span>Supervisor</span>
        </span>
      </div>

      <!-- 桥接卡 -->
      <div class="kpi-card">
        <div class="flex items-start justify-between">
          <span class="kpi-label">启用桥接</span>
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-accent-50 text-accent-600">
            <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M13.19 8.688a4.5 4.5 0 011.242 7.244l-4.5 4.5a4.5 4.5 0 01-6.364-6.364l1.757-1.757m13.35-.622l1.757-1.757a4.5 4.5 0 00-6.364-6.364l-4.5 4.5a4.5 4.5 0 001.242 7.244" />
            </svg>
          </span>
        </div>
        <p class="kpi-value">
          <span>{{ status?.enabled_bridge_count ?? '—' }}</span>
          <span class="text-base font-normal text-surface-400"> / {{ status?.total_bridge_count ?? '—' }}</span>
        </p>
        <span class="pill-info self-start">
          <span class="pill-dot"></span>
          <span>已激活同步</span>
        </span>
      </div>

      <!-- 凭据卡 -->
      <div class="kpi-card">
        <div class="flex items-start justify-between">
          <span class="kpi-label">凭据完整性</span>
          <span
            class="flex h-8 w-8 items-center justify-center rounded-lg"
            :class="status?.creds_complete ? 'bg-brand-50 text-brand-600' : 'bg-amber-50 text-amber-600'"
          >
            <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M9 12.75L11.25 15 15 9.75M21 12c0 1.268-.63 2.39-1.593 3.068a3.745 3.745 0 01-1.043 3.296 3.745 3.745 0 01-3.296 1.043A3.745 3.745 0 0112 21c-1.268 0-2.39-.63-3.068-1.593a3.746 3.746 0 01-3.296-1.043 3.745 3.745 0 01-1.043-3.296A3.745 3.745 0 013 12c0-1.268.63-2.39 1.593-3.068a3.745 3.745 0 011.043-3.296 3.746 3.746 0 013.296-1.043A3.746 3.746 0 0112 3c1.268 0 2.39.63 3.068 1.593a3.746 3.746 0 013.296 1.043 3.746 3.746 0 011.043 3.296A3.745 3.745 0 0121 12z" />
            </svg>
          </span>
        </div>
        <p
          class="kpi-value"
          :class="{
            'text-brand-600': credsState.tone === 'success',
            'text-amber-600': credsState.tone === 'warning',
            'text-surface-400': credsState.tone === 'neutral',
          }"
        >
          {{ credsState.text }}
        </p>
        <span
          class="self-start"
          :class="{
            'pill-success': credsState.tone === 'success',
            'pill-warning': credsState.tone === 'warning',
            'pill-neutral': credsState.tone === 'neutral',
          }"
        >
          <span class="pill-dot"></span>
          <span>Xboard / 3x-ui</span>
        </span>
      </div>

      <!-- 监听地址卡 -->
      <div class="kpi-card">
        <div class="flex items-start justify-between">
          <span class="kpi-label">监听地址</span>
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-violet-50 text-violet-600">
            <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M8.288 15.038a5.25 5.25 0 017.424 0M5.106 11.856c3.807-3.808 9.98-3.808 13.788 0M1.924 8.674c5.565-5.565 14.587-5.565 20.152 0M12.53 18.22l-.53.53-.53-.53a.75.75 0 011.06 0z" />
            </svg>
          </span>
        </div>
        <p class="break-all font-mono text-base font-medium text-surface-800">
          {{ status?.listen_addr || '—' }}
        </p>
        <span class="pill-info self-start">
          <span class="pill-dot"></span>
          <span>Web Panel</span>
        </span>
      </div>
    </section>

    <!-- 桥接概览表格 -->
    <section class="card">
      <header class="section-title">
        <span class="section-title-icon">
          <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round"
              d="M3 7.5h18M3 12h18M3 16.5h18" />
          </svg>
        </span>
        <div class="flex-1">
          <h3 class="section-title-text">桥接概览</h3>
          <p class="section-title-subtitle">所有已配置的 Xboard ↔ 3x-ui 桥接</p>
        </div>
      </header>

      <div v-if="!loading && bridges.length === 0" class="rounded-xl border border-dashed border-surface-300 bg-surface-50/50 px-6 py-10 text-center">
        <svg class="mx-auto mb-3 h-10 w-10 text-surface-400" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round"
            d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9 5.25h.008v.008H12v-.008z" />
        </svg>
        <p class="text-sm text-surface-600">尚未配置任何桥接</p>
        <p class="mt-1 text-xs text-surface-500">
          请前往 <RouterLink to="/bridges" class="font-medium text-brand-600 hover:text-brand-700">桥接管理</RouterLink> 添加。
        </p>
      </div>

      <div v-else-if="bridges.length > 0" class="overflow-x-auto">
        <table class="data-table">
          <thead>
            <tr>
              <th>名称</th>
              <th>协议</th>
              <th>Xboard 节点</th>
              <th>3x-ui inbound</th>
              <th>状态</th>
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
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </div>
</template>
