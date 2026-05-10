<script setup lang="ts">
// 运行参数页：Xboard / 3x-ui / 间隔 / 上报 / 日志 五个分组。
//
// Web 字段（listen_addr / session_max_age_hours / absolute_max_lifetime_hours）
// 由后端拒绝运行时修改——本页面把它们标记为只读 + 显示提示。
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
    <div class="flex items-center justify-between mb-6">
      <h2 class="text-2xl font-bold">运行参数</h2>
      <div class="space-x-2">
        <button class="btn-secondary" @click="refresh" :disabled="loading">刷新</button>
        <button class="btn-primary" @click="submit" :disabled="submitting">
          {{ submitting ? '保存中…' : '保存' }}
        </button>
      </div>
    </div>

    <div v-if="errMsg" class="card mb-4 border-red-200 bg-red-50 text-red-700">{{ errMsg }}</div>
    <div v-if="okMsg" class="card mb-4 border-emerald-200 bg-emerald-50 text-emerald-700">{{ okMsg }}</div>

    <div class="space-y-6">
      <!-- Xboard -->
      <div class="card">
        <h3 class="text-lg font-semibold mb-4">Xboard 面板对接</h3>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label class="label">api_host</label>
            <input v-model="form.xboard.api_host" class="input" placeholder="https://panel.example.com" />
          </div>
          <div>
            <label class="label">token (server_token)</label>
            <input v-model="form.xboard.token" class="input" />
          </div>
          <div>
            <label class="label">timeout_sec</label>
            <input v-model.number="form.xboard.timeout_sec" type="number" min="1" class="input" />
          </div>
          <div>
            <label class="label">user_agent</label>
            <input v-model="form.xboard.user_agent" class="input" />
          </div>
          <div class="md:col-span-2">
            <label class="flex items-center gap-2">
              <input v-model="form.xboard.skip_tls_verify" type="checkbox" />
              <span class="text-sm">skip_tls_verify（仅自签证书内网部署可开启）</span>
            </label>
          </div>
        </div>
      </div>

      <!-- 3x-ui -->
      <div class="card">
        <h3 class="text-lg font-semibold mb-4">3x-ui 面板对接</h3>
        <p class="text-sm text-gray-600 mb-4">
          v0.4 起仅支持账号密码登录（cookie 模式）。Bearer Token 模式已移除——若你从 v0.2/v0.3 升级，旧的 <code class="bg-gray-100 px-1">api_token</code> 设置已被忽略，请在下方填 3x-ui 后台用户名 + 密码。
        </p>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label class="label">api_host</label>
            <input v-model="form.xui.api_host" class="input" placeholder="http://127.0.0.1:2053" />
          </div>
          <div>
            <label class="label">base_path（webBasePath）</label>
            <input v-model="form.xui.base_path" class="input" placeholder="留空表示 /" />
          </div>
          <div>
            <label class="label">username（3x-ui 后台用户名）</label>
            <input v-model="form.xui.username" class="input" autocomplete="off" />
          </div>
          <div>
            <label class="label">password（3x-ui 后台密码）</label>
            <input v-model="form.xui.password" type="password" class="input" autocomplete="new-password" />
          </div>
          <div class="md:col-span-2">
            <label class="label">totp_secret（仅 3x-ui 启用了 2FA 时填；base32 secret）</label>
            <input
              v-model="form.xui.totp_secret"
              type="password"
              class="input"
              placeholder="留空 = 未启用 2FA（默认情形）"
              autocomplete="off"
            />
            <p class="text-xs text-gray-500 mt-1">
              需保证本机系统时钟与 3x-ui 主机时钟相差小于 30 秒——TOTP 算法对时钟漂移敏感（依赖 NTP 同步）。
            </p>
          </div>
          <div>
            <label class="label">timeout_sec</label>
            <input v-model.number="form.xui.timeout_sec" type="number" min="1" class="input" />
          </div>
          <div class="md:col-span-2">
            <label class="flex items-center gap-2">
              <input v-model="form.xui.skip_tls_verify" type="checkbox" />
              <span class="text-sm">skip_tls_verify</span>
            </label>
          </div>
        </div>
      </div>

      <!-- 间隔 -->
      <div class="card">
        <h3 class="text-lg font-semibold mb-4">同步周期（秒）</h3>
        <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
          <div>
            <label class="label">user_pull_sec</label>
            <input v-model.number="form.intervals.user_pull_sec" type="number" min="5" class="input" />
          </div>
          <div>
            <label class="label">traffic_push_sec</label>
            <input v-model.number="form.intervals.traffic_push_sec" type="number" min="5" class="input" />
          </div>
          <div>
            <label class="label">alive_push_sec</label>
            <input v-model.number="form.intervals.alive_push_sec" type="number" min="5" class="input" />
          </div>
          <div>
            <label class="label">status_push_sec</label>
            <input v-model.number="form.intervals.status_push_sec" type="number" min="5" class="input" />
          </div>
        </div>
      </div>

      <!-- 上报开关 -->
      <div class="card">
        <h3 class="text-lg font-semibold mb-4">可选上报</h3>
        <div class="space-y-2">
          <label class="flex items-center gap-2">
            <input v-model="form.reporting.alive_enabled" type="checkbox" />
            <span class="text-sm">在线 IP 上报（用于 Xboard 设备数限制）</span>
          </label>
          <label class="flex items-center gap-2">
            <input v-model="form.reporting.status_enabled" type="checkbox" />
            <span class="text-sm">节点 CPU / 内存 / 磁盘上报</span>
          </label>
        </div>
      </div>

      <!-- 日志 -->
      <div class="card">
        <h3 class="text-lg font-semibold mb-4">日志</h3>
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div>
            <label class="label">level</label>
            <select v-model="form.log.level" class="input">
              <option value="debug">debug</option>
              <option value="info">info</option>
              <option value="warn">warn</option>
              <option value="error">error</option>
            </select>
          </div>
          <div>
            <label class="label">file（留空 = stdout）</label>
            <input v-model="form.log.file" class="input" placeholder="例如 ./data/logs/bridge.log" />
          </div>
          <div>
            <label class="label">max_size_mb</label>
            <input v-model.number="form.log.max_size_mb" type="number" min="0" class="input" />
          </div>
          <div>
            <label class="label">max_backups</label>
            <input v-model.number="form.log.max_backups" type="number" min="0" class="input" />
          </div>
          <div>
            <label class="label">max_age_days</label>
            <input v-model.number="form.log.max_age_days" type="number" min="0" class="input" />
          </div>
        </div>
      </div>

      <!-- Web（只读） -->
      <div class="card border-amber-200">
        <h3 class="text-lg font-semibold mb-2">Web 面板（重启生效）</h3>
        <p class="text-sm text-amber-700 mb-4">
          以下字段在运行期不可修改——它们在进程启动时被消费。如需调整，请用 <code class="bg-gray-100 px-1">sqlite3 ./data/bridge.db</code> 直接编辑 settings 表后重启进程。
        </p>
        <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
          <div>
            <label class="label">listen_addr</label>
            <input :value="form.web.listen_addr" class="input bg-gray-50" disabled />
          </div>
          <div>
            <label class="label">session_max_age_hours</label>
            <input :value="form.web.session_max_age_hours" class="input bg-gray-50" disabled />
          </div>
          <div>
            <label class="label">absolute_max_lifetime_hours</label>
            <input :value="form.web.absolute_max_lifetime_hours" class="input bg-gray-50" disabled />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
