import { inject, onBeforeUnmount, provide, ref, type InjectionKey } from 'vue'
import { api, ApiError } from './api'

export interface BuildStatus {
  id?: number
  queued: number
  state: 'idle' | 'running' | 'success' | 'failed'
  enabled: boolean
  startedAt?: string
  finishedAt?: string
  log: string
  error?: string
}

export interface BuildRecord {
  id: number
  state: BuildStatus['state']
  startedAt: string
  finishedAt: string
  error: string
}

export const buildStateLabel = { idle: '尚未发布', running: '正在发布', success: '发布成功', failed: '发布失败' }

export async function listBuildHistory(signal: AbortSignal) {
  const data = await api<{ builds: BuildRecord[] }>('/admin/build/history', { signal })
  return data.builds
}

const publishKey: InjectionKey<ReturnType<typeof provideAdminPublish>> = Symbol('adminPublish')

export function provideAdminPublish() {
  const status = ref<BuildStatus>()
  const pending = ref(true)
  const starting = ref(false)
  const error = ref('')
  const requests = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined

  function schedule() {
    clearTimeout(timer)
    if (!requests.signal.aborted) timer = setTimeout(refresh, 2500)
  }

  async function refresh() {
    clearTimeout(timer)
    try {
      const data = await api<BuildStatus>('/admin/build', { signal: requests.signal })
      if (requests.signal.aborted) return
      status.value = data
      error.value = ''
      if (data.state === 'running') schedule()
    } catch (reason) {
      if (requests.signal.aborted) return
      error.value = reason instanceof Error ? reason.message : '读取发布状态失败'
      if (status.value?.state === 'running' && !(reason instanceof ApiError && [401, 403].includes(reason.status))) schedule()
    } finally {
      if (!requests.signal.aborted) pending.value = false
    }
  }

  async function start() {
    if (starting.value || !status.value?.enabled) return
    starting.value = true
    error.value = ''
    try {
      status.value = await api<BuildStatus>('/admin/build', { method: 'POST', signal: requests.signal })
      schedule()
    } catch (reason) {
      if (requests.signal.aborted) return
      error.value = reason instanceof Error ? reason.message : '发布任务入队失败'
    } finally {
      starting.value = false
    }
  }

  const state = { status, pending, starting, error, start, refresh }
  provide(publishKey, state)
  onBeforeUnmount(() => {
    requests.abort()
    clearTimeout(timer)
  })
  return state
}

export function useAdminPublish() {
  const state = inject(publishKey)
  if (!state) throw new Error('AdminPublish requires AdminShell')
  return state
}
