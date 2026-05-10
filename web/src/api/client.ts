// 统一 API 客户端。
//
// 设计要点：
//
//   1. 用 fetch + cookie 模式：与后端 SessionCookieName=bridge_session
//      约定一致，浏览器自动处理 cookie。
//   2. 错误统一抛 ApiError：调用方只需 try/catch 即可分类处理（4xx 提示
//      运维，5xx 提示运维 + 上报）。
//   3. 401 由全局 handler 集中处理：详见 setUnauthorizedHandler。
//   4. 不依赖 axios：减少依赖，标准 fetch 已经够用。
//
// 后端响应外壳（详见 internal/web/json.go）：
//   { "data": { ... } }   成功路径
//   { "error": { "code": "...", "message": "..." } }   失败路径
//
// 客户端行为：
//   - 成功 → 返回 data 部分
//   - 失败 → 401 时先触发全局 handler（清空登录态 + 跳登录页），
//           然后抛 ApiError 让调用方继续走错误路径
//   - 其它失败 → 直接抛 ApiError

export interface ApiErrorPayload {
  code: string
  message: string
}

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  // skipAuthHandler=true 让 fetchMe / login 自身的 401 不触发"全局踢回登录页"
  // 行为——它们正在判定登录状态本身，不应被全局 handler 干扰。
  skipAuthHandler?: boolean
}

// onUnauthorized 是登录态失效时的全局回调，由 main.ts 在装配时注入。
//
// 不让 client.ts 直接 import auth store / router：
//
//   a) 避免循环导入（store import api，api import store 会让 vite 多花一道
//      tree-shaking 工作）；
//   b) 让 client.ts 保持"无业务依赖"——未来要在 jest 单测里 mock 它会更轻；
//   c) 调用方可在 main.ts 里用一行 closure 把"清空 auth + 路由跳登录页"
//      绑进来，符合"依赖注入"思想。
let onUnauthorized: (() => void) | null = null

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const init: RequestInit = {
    method: opts.method ?? 'GET',
    credentials: 'same-origin', // 与 cookie SameSite=Lax 一致
    headers: {
      Accept: 'application/json',
    },
  }
  if (opts.body !== undefined) {
    init.body = JSON.stringify(opts.body)
    init.headers = { ...init.headers, 'Content-Type': 'application/json' }
  }

  const resp = await fetch(path, init)
  // 204 / 空 body 路径：M5 后端约定所有删除返回 200 + {}，所以这里
  // 应当不会遇到 204；保留兜底以防未来后端改回。
  if (resp.status === 204) {
    return undefined as T
  }
  let payload: { data?: T; error?: ApiErrorPayload } = {}
  try {
    payload = (await resp.json()) as typeof payload
  } catch {
    throw new ApiError(resp.status, 'invalid_response', '响应解析失败')
  }
  if (!resp.ok) {
    // 401 路径：触发全局 handler 让 UI 立刻进入未登录状态。skipAuthHandler
    // 用于 fetchMe / login 自身——它们就是用来判定登录态的，不能被自己
    // 触发的 401 反复踢回登录页。
    if (resp.status === 401 && !opts.skipAuthHandler && onUnauthorized) {
      onUnauthorized()
    }
    const err = payload.error ?? { code: 'unknown', message: `HTTP ${resp.status}` }
    throw new ApiError(resp.status, err.code, err.message)
  }
  return payload.data as T
}

// =====================================================================
// 业务接口集合
// =====================================================================

export interface User {
  id: number
  username: string
  last_login_at?: string
}

export interface Bridge {
  name: string
  xboard_node_id: number
  xboard_node_type: string
  xui_inbound_id: number
  protocol: string
  flow?: string
  enable: boolean
  created_at?: string
  updated_at?: string
}

export interface Settings {
  log: { level: string; file: string; max_size_mb: number; max_backups: number; max_age_days: number }
  xboard: { api_host: string; token: string; timeout_sec: number; skip_tls_verify: boolean; user_agent: string }
  // xui v0.6 起仅 Bearer API Token 单通道（仅适配 3x-ui v3.0.0+）。
  // 账号密码 / cookie / CSRF / TOTP 路径已彻底移除——旧 settings 表里残留
  // 的 username / password / totp_secret / auth_mode 行被后端 LoadFromStore
  // 忽略，前端类型也不再暴露。
  xui: {
    api_host: string
    base_path: string
    api_token: string
    timeout_sec: number
    skip_tls_verify: boolean
  }
  intervals: { user_pull_sec: number; traffic_push_sec: number; alive_push_sec: number; status_push_sec: number }
  reporting: { alive_enabled: boolean; status_enabled: boolean }
  web: { listen_addr: string; session_max_age_hours: number; absolute_max_lifetime_hours: number }
}

export type SettingsPatch = {
  [K in keyof Settings]?: Partial<Settings[K]>
}

export interface Status {
  running: boolean
  enabled_bridge_count: number
  total_bridge_count: number
  creds_complete: boolean
  listen_addr: string
  now: string
}

export const api = {
  // ---- auth ----
  // login / me 走 skipAuthHandler：自身就是用来判定登录态，不能被
  // 自己引发的 401 反复踢回登录页。
  login(username: string, password: string) {
    return request<User>('/api/auth/login', {
      method: 'POST',
      body: { username, password },
      skipAuthHandler: true,
    })
  },
  logout() {
    return request<void>('/api/auth/logout', { method: 'POST', skipAuthHandler: true })
  },
  me() {
    return request<User>('/api/auth/me', { method: 'GET', skipAuthHandler: true })
  },
  changePassword(oldPassword: string, newPassword: string) {
    return request<void>('/api/account/password', {
      method: 'PUT',
      body: { old_password: oldPassword, new_password: newPassword },
    })
  },

  // ---- settings ----
  getSettings() {
    return request<Settings>('/api/settings')
  },
  patchSettings(patch: SettingsPatch) {
    return request<void>('/api/settings', { method: 'PATCH', body: patch })
  },

  // ---- bridges ----
  listBridges() {
    return request<Bridge[]>('/api/bridges')
  },
  createBridge(b: Omit<Bridge, 'created_at' | 'updated_at'>) {
    return request<Bridge>('/api/bridges', { method: 'POST', body: b })
  },
  updateBridge(name: string, b: Omit<Bridge, 'created_at' | 'updated_at'>) {
    return request<Bridge>(`/api/bridges/${encodeURIComponent(name)}`, { method: 'PUT', body: b })
  },
  deleteBridge(name: string) {
    return request<void>(`/api/bridges/${encodeURIComponent(name)}`, { method: 'DELETE' })
  },

  // ---- status ----
  getStatus() {
    return request<Status>('/api/status')
  },
}
