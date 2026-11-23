import { inject, onBeforeUnmount, provide, ref, type InjectionKey } from 'vue'
import { api, ApiError } from './api'

export type BuildRequest =
  | { mode: 'incremental' | 'all', noteId?: never }
  | { mode: 'note', noteId: string }

export type BuildTask = BuildRequest & { id: number }
export type BuildReceipt = BuildTask & { state: 'running' | 'queued' }

export interface BuildStatus {
  id?: number
  mode?: BuildRequest['mode']
  noteId?: string
  queued: number
  queue?: BuildTask[]
  state: 'idle' | 'running' | 'success' | 'failed'
  enabled: boolean
  startedAt?: string
  finishedAt?: string
  log: string
  error?: string
}

export type BuildRecord = BuildTask & {
  state: BuildStatus['state']
  startedAt: string
  finishedAt: string
  error: string
}

export const buildStateLabel = { idle: '尚未发布', running: '正在发布', success: '发布成功', failed: '发布失败' }

export function buildTaskLabel(task: { mode?: BuildRequest['mode'], noteId?: string }) {
  if (task.mode === 'all') return '全站重建'
  if (task.mode === 'note') return task.noteId?.split('/').at(-1) || '单篇重建'
  return '增量发布'
}

export function buildResultLabel(task: Pick<BuildStatus, 'mode' | 'state'>) {
  if (task.mode === 'note' && task.state !== 'idle') {
    return { running: '正在重建', success: '重建完成', failed: '重建失败' }[task.state]
  }
  return buildStateLabel[task.state]
}

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
  let refreshVersion = 0

  function schedule() {
    clearTimeout(timer)
    if (!requests.signal.aborted) timer = setTimeout(refresh, 2500)
  }

  async function refresh() {
    clearTimeout(timer)
    const version = ++refreshVersion
    try {
      const data = await api<BuildStatus>('/admin/build', { signal: requests.signal })
      if (requests.signal.aborted || version !== refreshVersion) return
      status.value = data
      error.value = ''
      if (data.state === 'running') schedule()
    } catch (reason) {
      if (requests.signal.aborted || version !== refreshVersion) return
      error.value = reason instanceof Error ? reason.message : '读取发布状态失败'
      if (status.value?.state === 'running' && !(reason instanceof ApiError && [401, 403].includes(reason.status))) schedule()
    } finally {
      if (!requests.signal.aborted && version === refreshVersion) pending.value = false
    }
  }

  async function start(request: BuildRequest) {
    if (starting.value || !status.value?.enabled) throw new Error('当前无法提交构建')
    starting.value = true
    error.value = ''
    try {
      const task = await api<BuildReceipt>('/admin/build', { method: 'POST', json: request, signal: requests.signal })
      void refresh()
      return task
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
