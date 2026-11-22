import { reactive } from 'vue'
import { api } from '@/lib/api'

export interface AccountUser {
  id: string
  email: string
  username: string
  role: 'admin' | 'user'
  status: 'active' | 'disabled'
  emailVerified: boolean
  isAdmin: boolean
  createdAt: string
  lastLoginAt?: string
}

export interface SessionState {
  user: AccountUser | null
  ready: boolean
}

export const session = reactive<SessionState>({ user: null, ready: false })

type SessionListener = (user: AccountUser | null) => void
const listeners = new Set<SessionListener>()
let pendingSession: Promise<void> | undefined
let sessionVersion = 0

export function onUserChange(listener: SessionListener) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function setSessionUser(user: AccountUser | null) {
  sessionVersion++
  session.user = user
  session.ready = true
  for (const listener of listeners) listener(user)
}

export function refreshSession() {
  if (typeof window === 'undefined') return
  if (pendingSession) return pendingSession
  const version = sessionVersion
  pendingSession = api<{ user: AccountUser | null }>('/api/auth/session')
    .then(data => data.user, () => null)
    .then(user => {
      if (version === sessionVersion) setSessionUser(user)
    })
    .finally(() => { pendingSession = undefined })
  return pendingSession
}
