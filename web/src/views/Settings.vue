<script setup lang="ts">
// 运行参数（v0.5 视觉重构）。
//
// 视觉策略：
//   - 每组配置（Xboard / 3x-ui / 同步周期 / 上报开关 / 日志 / Web）独立卡片，
//     卡片头部有图标徽章 + 标题 + 副标题——让多组配置一目了然。
//   - 顶部"操作栏"sticky：保存按钮始终可见，不必滚到最底才能保存。
//   - Web 字段（只读）卡片用琥珀色边框 + 内部说明，与可编辑组视觉区分。
//
// 可访问性约定（v0.5 严格化）：
//   - 所有 input 必须带 id，配套 label 必须带 for——让屏幕阅读器能朗读
//     字段名，让用户点击 label 文字也能聚焦输入框。id 命名约定 s-<group>-<field>，
//     防止与其它页面的同名 id 冲突。
//   - 装饰性 SVG 一律 aria-hidden="true"——文字标签已经表达语义，图标
//     在辅助技术树里被屏蔽即可。
//   - 原生 checkbox 用 `accent-brand-500` 着色——这是 CSS accent-color 属性
//     的 Tailwind 工具类，在所有现代浏览器都生效；旧 `text-brand-600` 仅对
//     第三方 forms 插件渲染的伪 checkbox 有意义，原生 checkbox 上无作用。
import { ref, onMounted, reactive } from 'vue'
import { api, ApiError, type Settings, type SettingsPatch } from '@/api/client'

const loading = ref(true)
const submitting = ref(false)
const errMsg = ref('')
const okMsg = ref('')

const original = ref<Settings | null>(null)
const form = reactive({
  log: { level: 'info', file: '', max_size_mb: 0, max_backups: 0, max_age_days: 0 },
  xboard: { api_host: '', token: '', timeout_sec: 15, skip_tls_verify: false, user_agent: '' },
  // xui form：v0.4 起仅 cookie 登录模式，固定字段集（无 auth_mode / api_token）。
  xui: {
    api_host: '',
    base_path: '',
    username: '',
    password: '',
    totp_secret: '',
    timeout_sec: 15,
    skip_tls_verify: false,
  },
  intervals: { user_pull_sec: 60, traffic_push_sec: 60, alive_push_sec: 60, status_push_sec: 60 },
  reporting: { alive_enabled: false, status_enabled: false },
  web: { listen_addr: '', session_max_age_hours: 0, absolute_max_lifetime_hours: 0 },
})

async function refresh() {
  loading.value = true
  errMsg.value = ''
  try {
    const s = await api.getSettings()
    original.value = s
    Object.assign(form.log, s.log)
    Object.assign(form.xboard, s.xboard)
    Object.assign(form.xui, s.xui)
    Object.assign(form.intervals, s.intervals)
    Object.assign(form.reporting, s.reporting)
    Object.assign(form.web, s.web)
  } catch (e) {
    errMsg.value = '加载配置失败'
    console.warn(e)
  } finally {
    loading.value = false
  }
}

// 计算 patch：仅当字段值与 original 不同时才放入 patch 中。
// 避免把"当前值"全量重写到 store，减少 reload 触发面。
function computePatch(): SettingsPatch {
  if (!original.value) return {}
  const patch: SettingsPatch = {}
  const groups: (keyof Settings)[] = ['log', 'xboard', 'xui', 'intervals', 'reporting']
  for (const g of groups) {
    const cur = form[g] as Record<string, unknown>
    const orig = original.value[g] as Record<string, unknown>
    const diff: Record<string, unknown> = {}
    for (const k of Object.keys(cur)) {
      if (cur[k] !== orig[k]) diff[k] = cur[k]
    }
    if (Object.keys(diff).length > 0) {
      ;(patch as Record<string, unknown>)[g] = diff
    }
  }
  return patch
}

async function submit() {
  errMsg.value = ''
  okMsg.value = ''
  const patch = computePatch()
  if (Object.keys(patch).length === 0) {
    errMsg.value = '没有需要保存的改动'
    return
  }
  submitting.value = true
  try {
    await api.patchSettings(patch)
    okMsg.value = '配置已保存并触发引擎热重载'
    await refresh()
  } catch (e) {
    errMsg.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    submitting.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7 flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-surface-900">运行参数</h2>
        <p class="mt-1 text-sm text-surface-500">维护 Xboard / 3x-ui 凭据、同步周期、上报开关、日志与 Web 设置</p>
      </div>
      <div class="flex items-center gap-2">
        <button class="btn-secondary" @click="refresh" :disabled="loading">
          <svg class="h-4 w-4" :class="{ 'animate-spin': loading }" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round"
              d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" />
          </svg>
          刷新
        </button>
        <button class="btn-primary" @click="submit" :disabled="submitting">
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
          <svg v-else class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round"
              d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5m-13.5-9L12 3m0 0l4.5 4.5M12 3v13.5" />
          </svg>
          {{ submitting ? '保存中…' : '保存' }}
        </button>
      </div>
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

    <div class="space-y-5">
      <!-- Xboard -->
      <section class="card">
        <header class="section-title">
          <span class="section-title-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z M3.6 9h16.8 M3.6 15h16.8 M11.5 3a17 17 0 000 18 M12.5 3a17 17 0 010 18" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">Xboard 面板对接</h3>
            <p class="section-title-subtitle">销售面板的访问凭据（server_token + API host）</p>
          </div>
        </header>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="label" for="s-xboard-api_host">api_host</label>
            <input id="s-xboard-api_host" v-model="form.xboard.api_host" class="input" placeholder="https://panel.example.com" />
          </div>
          <div>
            <label class="label" for="s-xboard-token">token (server_token)</label>
            <input id="s-xboard-token" v-model="form.xboard.token" type="password" class="input input-mono" autocomplete="off" />
          </div>
          <div>
            <label class="label" for="s-xboard-timeout_sec">timeout_sec</label>
            <input id="s-xboard-timeout_sec" v-model.number="form.xboard.timeout_sec" type="number" min="1" class="input input-mono" />
          </div>
          <div>
            <label class="label" for="s-xboard-user_agent">user_agent</label>
            <input id="s-xboard-user_agent" v-model="form.xboard.user_agent" class="input" />
          </div>
          <div class="md:col-span-2">
            <label class="flex items-center gap-2.5 cursor-pointer" for="s-xboard-skip_tls_verify">
              <input id="s-xboard-skip_tls_verify" v-model="form.xboard.skip_tls_verify" type="checkbox" class="cb" />
              <span class="text-sm text-surface-700">skip_tls_verify <span class="text-xs text-surface-500">（仅自签证书内网部署可开启）</span></span>
            </label>
          </div>
        </div>
      </section>

      <!-- 3x-ui -->
      <section class="card">
        <header class="section-title">
          <span class="section-title-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M9 17.25v1.007a3 3 0 01-.879 2.122L7.5 21h9l-.621-.621A3 3 0 0115 18.257V17.25m6-12V15a2.25 2.25 0 01-2.25 2.25H5.25A2.25 2.25 0 013 15V5.25m18 0A2.25 2.25 0 0018.75 3H5.25A2.25 2.25 0 003 5.25m18 0V12a2.25 2.25 0 01-2.25 2.25H5.25A2.25 2.25 0 013 12V5.25" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">3x-ui 面板对接</h3>
            <p class="section-title-subtitle">节点端面板的 cookie 登录凭据（仅账号密码模式）</p>
          </div>
        </header>
        <div class="alert-info mb-5">
          <svg class="h-5 w-5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round"
              d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" />
          </svg>
          <span class="leading-relaxed">
            v0.4 起仅支持账号密码登录（cookie 模式）。Bearer Token 模式已彻底移除——若你从 v0.2/v0.3 升级，旧的
            <code class="rounded bg-white/60 px-1.5 py-0.5 font-mono text-[12px]">api_token</code>
            设置已被忽略，请填 3x-ui 后台用户名 + 密码。
          </span>
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="label" for="s-xui-api_host">api_host</label>
            <input id="s-xui-api_host" v-model="form.xui.api_host" class="input" placeholder="http://127.0.0.1:2053" />
          </div>
          <div>
            <label class="label" for="s-xui-base_path">base_path（webBasePath）</label>
            <input id="s-xui-base_path" v-model="form.xui.base_path" class="input" placeholder="留空表示 /" />
          </div>
          <div>
            <label class="label" for="s-xui-username">username（3x-ui 后台用户名）</label>
            <input id="s-xui-username" v-model="form.xui.username" class="input" autocomplete="off" />
          </div>
          <div>
            <label class="label" for="s-xui-password">password（3x-ui 后台密码）</label>
            <input id="s-xui-password" v-model="form.xui.password" type="password" class="input" autocomplete="new-password" />
          </div>
          <div class="md:col-span-2">
            <label class="label" for="s-xui-totp_secret">totp_secret <span class="text-xs font-normal text-surface-500">（仅 3x-ui 启用了 2FA 时填；base32 secret）</span></label>
            <input
              id="s-xui-totp_secret"
              v-model="form.xui.totp_secret"
              type="password"
              class="input input-mono"
              placeholder="留空 = 未启用 2FA（默认情形）"
              autocomplete="off"
            />
            <p class="help-text">
              需保证本机系统时钟与 3x-ui 主机时钟相差小于 30 秒——TOTP 算法对时钟漂移敏感（依赖 NTP 同步）。
            </p>
          </div>
          <div>
            <label class="label" for="s-xui-timeout_sec">timeout_sec</label>
            <input id="s-xui-timeout_sec" v-model.number="form.xui.timeout_sec" type="number" min="1" class="input input-mono" />
          </div>
          <div class="md:col-span-2">
            <label class="flex items-center gap-2.5 cursor-pointer" for="s-xui-skip_tls_verify">
              <input id="s-xui-skip_tls_verify" v-model="form.xui.skip_tls_verify" type="checkbox" class="cb" />
              <span class="text-sm text-surface-700">skip_tls_verify</span>
            </label>
          </div>
        </div>
      </section>

      <!-- 同步周期 -->
      <section class="card">
        <header class="section-title">
          <span class="section-title-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">同步周期（秒）</h3>
            <p class="section-title-subtitle">最低 5 秒，避免对上游 API 形成压测；默认 60 秒与 Xboard 推荐一致</p>
          </div>
        </header>
        <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
          <div>
            <label class="label" for="s-int-user_pull_sec">user_pull_sec</label>
            <input id="s-int-user_pull_sec" v-model.number="form.intervals.user_pull_sec" type="number" min="5" class="input input-mono" />
          </div>
          <div>
            <label class="label" for="s-int-traffic_push_sec">traffic_push_sec</label>
            <input id="s-int-traffic_push_sec" v-model.number="form.intervals.traffic_push_sec" type="number" min="5" class="input input-mono" />
          </div>
          <div>
            <label class="label" for="s-int-alive_push_sec">alive_push_sec</label>
            <input id="s-int-alive_push_sec" v-model.number="form.intervals.alive_push_sec" type="number" min="5" class="input input-mono" />
          </div>
          <div>
            <label class="label" for="s-int-status_push_sec">status_push_sec</label>
            <input id="s-int-status_push_sec" v-model.number="form.intervals.status_push_sec" type="number" min="5" class="input input-mono" />
          </div>
        </div>
      </section>

      <!-- 上报开关 -->
      <section class="card">
        <header class="section-title">
          <span class="section-title-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M9.348 14.652a3.75 3.75 0 010-5.304m5.304 0a3.75 3.75 0 010 5.304m-7.425 2.121a6.75 6.75 0 010-9.546m9.546 0a6.75 6.75 0 010 9.546M5.106 18.894c-3.808-3.807-3.808-9.98 0-13.788m13.788 0c3.808 3.807 3.808 9.98 0 13.788M12 12h.008v.008H12V12zm.375 0a.375.375 0 11-.75 0 .375.375 0 01.75 0z" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">可选上报</h3>
            <p class="section-title-subtitle">在线 IP / 节点负载——按需启用</p>
          </div>
        </header>
        <div class="space-y-3">
          <label class="flex items-start gap-2.5 cursor-pointer rounded-xl border border-surface-200 p-4 hover:border-surface-300 transition-colors" for="s-rep-alive_enabled">
            <input id="s-rep-alive_enabled" v-model="form.reporting.alive_enabled" type="checkbox" class="cb mt-0.5" />
            <div class="flex-1">
              <p class="text-sm font-medium text-surface-800">在线 IP 上报</p>
              <p class="mt-0.5 text-xs text-surface-500">用于 Xboard 设备数限制；逐 email 串行调用 3x-ui，inbound 内在线用户多时延迟显著（限并发 8 路）。</p>
            </div>
          </label>
          <label class="flex items-start gap-2.5 cursor-pointer rounded-xl border border-surface-200 p-4 hover:border-surface-300 transition-colors" for="s-rep-status_enabled">
            <input id="s-rep-status_enabled" v-model="form.reporting.status_enabled" type="checkbox" class="cb mt-0.5" />
            <div class="flex-1">
              <p class="text-sm font-medium text-surface-800">节点 CPU / 内存 / 磁盘上报</p>
              <p class="mt-0.5 text-xs text-surface-500">从 3x-ui server/status 拉取节点负载；仅刷新 Xboard 后台的负载条，与节点在线状态判定无关。</p>
            </div>
          </label>
        </div>
      </section>

      <!-- 日志 -->
      <section class="card">
        <header class="section-title">
          <span class="section-title-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M19.5 14.25v-2.625a3.375 3.375 0 00-3.375-3.375h-1.5A1.125 1.125 0 0113.5 7.125v-1.5a3.375 3.375 0 00-3.375-3.375H8.25m0 12.75h7.5m-7.5 3H12M10.5 2.25H5.625c-.621 0-1.125.504-1.125 1.125v17.25c0 .621.504 1.125 1.125 1.125h12.75c.621 0 1.125-.504 1.125-1.125V11.25a9 9 0 00-9-9z" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">日志</h3>
            <p class="section-title-subtitle">level 支持热重载；file / max_size / max_backups / max_age_days 重启生效</p>
          </div>
        </header>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <div>
            <label class="label" for="s-log-level">level</label>
            <select id="s-log-level" v-model="form.log.level" class="input">
              <option value="debug">debug</option>
              <option value="info">info</option>
              <option value="warn">warn</option>
              <option value="error">error</option>
            </select>
          </div>
          <div>
            <label class="label" for="s-log-file">file（留空 = stdout）</label>
            <input id="s-log-file" v-model="form.log.file" class="input input-mono" placeholder="例如 ./data/logs/bridge.log" />
          </div>
          <div>
            <label class="label" for="s-log-max_size_mb">max_size_mb</label>
            <input id="s-log-max_size_mb" v-model.number="form.log.max_size_mb" type="number" min="0" class="input input-mono" />
          </div>
          <div>
            <label class="label" for="s-log-max_backups">max_backups</label>
            <input id="s-log-max_backups" v-model.number="form.log.max_backups" type="number" min="0" class="input input-mono" />
          </div>
          <div>
            <label class="label" for="s-log-max_age_days">max_age_days</label>
            <input id="s-log-max_age_days" v-model.number="form.log.max_age_days" type="number" min="0" class="input input-mono" />
          </div>
        </div>
      </section>

      <!-- Web（只读） -->
      <section class="card border-amber-200 bg-amber-50/30">
        <header class="section-title">
          <span class="flex h-9 w-9 items-center justify-center rounded-xl bg-amber-100 text-amber-700">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.75" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round"
                d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" />
            </svg>
          </span>
          <div class="flex-1">
            <h3 class="section-title-text">Web 面板（重启生效）</h3>
            <p class="section-title-subtitle text-amber-700">
              以下字段在运行期不可修改——它们在进程启动时被消费一次。
            </p>
          </div>
        </header>
        <p class="mb-4 rounded-xl border border-amber-200 bg-white/60 p-3 text-xs leading-relaxed text-amber-800">
          要改 listen_addr，最简单的方式是
          <code class="rounded bg-amber-100 px-1.5 py-0.5 font-mono">xui-bridge change-listen-addr</code>
          （写 systemd drop-in override + 重启）；其它启动期字段需用
          <code class="rounded bg-amber-100 px-1.5 py-0.5 font-mono">sqlite3 ./data/bridge.db</code>
          直接编辑 settings 表后重启进程。
        </p>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <div>
            <label class="label" for="s-web-listen_addr">listen_addr</label>
            <input id="s-web-listen_addr" :value="form.web.listen_addr" class="input input-mono" disabled />
          </div>
          <div>
            <label class="label" for="s-web-session_max_age_hours">session_max_age_hours</label>
            <input id="s-web-session_max_age_hours" :value="form.web.session_max_age_hours" class="input input-mono" disabled />
          </div>
          <div>
            <label class="label" for="s-web-absolute_max_lifetime_hours">absolute_max_lifetime_hours</label>
            <input id="s-web-absolute_max_lifetime_hours" :value="form.web.absolute_max_lifetime_hours" class="input input-mono" disabled />
          </div>
        </div>
      </section>
    </div>
  </div>
</template>
