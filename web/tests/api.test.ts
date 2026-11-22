import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, apiUrl } from '../src/lib/api'
import type { AccountUser } from '../src/lib/session'

afterEach(() => vi.unstubAllGlobals())

describe('JSON API', () => {
  it('serializes the body and uses the session cookie', async () => {
    const fetch = vi.fn().mockResolvedValue(Response.json({ ok: true }))
    vi.stubGlobal('fetch', fetch)

    await expect(api('/me/favorites', { method: 'PUT', json: { noteId: 'math/course/note' } }))
      .resolves.toEqual({ ok: true })
    const [path, request] = fetch.mock.calls[0]
    expect(path).toBe(`${import.meta.env.VITE_API_BASE_PATH}me/favorites`)
    expect(request.method).toBe('PUT')
    expect(request.body).toBe('{"noteId":"math/course/note"}')
    expect(request.headers.get('Content-Type')).toBe('application/json')
    expect(request.credentials).toBe('same-origin')
  })

  it.each([
    ['HTML from the proxy', '<html></html>', 'text/html'],
    ['malformed JSON', '{', 'application/json'],
  ])('reports %s as an invalid response', async (_name, body, contentType) => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(body, { headers: { 'Content-Type': contentType } })))
    await expect(api('/auth/session')).rejects.toMatchObject({ code: 'invalid_response', status: 200 })
  })

  it('preserves error codes and Retry-After', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(Response.json({ code: 'rate_limited', error: '稍后重试' }, {
      status: 429,
      headers: { 'Retry-After': '15' },
    })))
    await expect(api('/search')).rejects.toMatchObject({
      code: 'rate_limited', message: '稍后重试', status: 429, retryAfter: 15,
    })
  })

  it('keeps query parameters in direct download and preview links', () => {
    expect(apiUrl('/admin/asset/content?id=a%2Fb&trash=true'))
      .toBe(`${import.meta.env.VITE_API_BASE_PATH}admin/asset/content?id=a%2Fb&trash=true`)
  })
})

it('shares session reads and ignores a response after the user signs in', async () => {
  vi.stubGlobal('window', {})
  const { refreshSession, session, setSessionUser } = await import('../src/lib/session')
  let resolveSession!: (response: Response) => void
  const fetch = vi.fn(() => new Promise<Response>(resolve => { resolveSession = resolve }))
  vi.stubGlobal('fetch', fetch)
  const user: AccountUser = {
    id: '1', username: 'reader', email: 'reader@example.com', role: 'user', status: 'active',
    emailVerified: true, isAdmin: false, createdAt: '2026-01-01T00:00:00Z',
  }

  const refresh = refreshSession()
  expect(refreshSession()).toBe(refresh)
  expect(fetch).toHaveBeenCalledTimes(1)
  setSessionUser(user)
  resolveSession(Response.json({ user: null }))
  await refresh
  expect(session.user).toEqual(user)
})
