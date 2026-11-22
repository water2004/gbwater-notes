import { api } from '@/lib/api'
import { setSessionUser, type AccountUser } from '@/lib/session'

export interface RegisterInput {
  username: string
  email: string
  password: string
  code: string
}

export async function login(identifier: string, password: string) {
  const data = await api<{ user: AccountUser }>('/auth/login', {
    method: 'POST',
    json: { identifier, password },
  })
  setSessionUser(data.user)
  return data.user
}

export async function register(input: RegisterInput) {
  const data = await api<{ user: AccountUser }>('/auth/register', {
    method: 'POST',
    json: input,
  })
  setSessionUser(data.user)
  return data.user
}

export async function logout() {
  try {
    await api('/auth/logout', { method: 'POST' })
  } finally {
    setSessionUser(null)
  }
}

export async function sendEmailCode(email: string, purpose: 'register' | 'reset') {
  return api<{ ok: boolean; cooldownSeconds: number }>('/auth/email-code', {
    method: 'POST',
    json: { email, purpose },
  })
}

export async function forgotPassword(email: string, code: string, password: string) {
  return api<{ ok: boolean }>('/auth/password/forgot', {
    method: 'POST',
    json: { email, code, password },
  })
}

export async function changePassword(currentPassword: string, newPassword: string) {
  return api<{ ok: boolean }>('/auth/password/change', {
    method: 'POST',
    json: { currentPassword, newPassword },
  })
}

export async function renameAccount(username: string) {
  const data = await api<{ user: AccountUser }>('/auth/profile', {
    method: 'PATCH',
    json: { username },
  })
  setSessionUser(data.user)
  return data.user
}
