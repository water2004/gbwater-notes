import { computed, nextTick, onBeforeUnmount, reactive, ref, shallowRef, watch, type Ref } from 'vue'
import { ApiError } from '@/lib/api'
import type { NoteMeta } from '@/types/content'
import { session } from '@/lib/session'
import {
  anchorForTextRange, applyBookmarks, applyHighlights, applyMarkdownHighlights, clearHighlights, createBookmark, createHighlight, createStickyNote, deleteBookmark,
  deleteHighlight, deleteStickyNote, elementAtOffset, headingText, indexArticle, loadAnnotations, locate,
  saveMemo, selectionAnchor, textRange, updateBookmark, updateStickyNote,
  type AnchorInput, type Bookmark, type Highlight, type HighlightColor, type NoteAnnotations, type StickyNote, type TextLocation,
} from '@/lib/annotations'

interface SelectionState {
  anchor: AnchorInput
  x: number
  y: number
}

export type AnnotationTab = 'highlights' | 'bookmarks' | 'sticky' | 'memo'

export function useNoteAnnotations(article: Ref<HTMLElement | undefined>, note: Ref<NoteMeta | undefined>) {
  const state = reactive({
    open: false,
    tab: 'highlights' as AnnotationTab,
    data: null as NoteAnnotations | null,
    loading: false,
    busy: false,
    notice: '',
    memoDraft: '',
    memoStatus: '',
    activeHighlight: '',
  })
  const selection = ref<SelectionState | null>(null)
  const locations = shallowRef(new Map<string, TextLocation>())
  const stickyAnchors = ref<Record<string, { x: number; y: number }>>({})
  const floatingStickyNotes = computed(() => state.data?.stickyNotes.filter(item => stickyAnchors.value[item.id]) ?? [])
  const selectionColor = ref<HighlightColor>('yellow')
  let memoTimer: ReturnType<typeof setTimeout> | undefined
  let noticeTimer: ReturnType<typeof setTimeout> | undefined
  let refreshController: AbortController | undefined

  const loggedIn = computed(() => Boolean(session.user))

  function notify(message: string) {
    state.notice = message
    clearTimeout(noticeTimer)
    noticeTimer = setTimeout(() => { state.notice = '' }, 3600)
  }

  function index() {
    return article.value ? indexArticle(article.value) : null
  }

  // 重绘包括划线、书签、便条锚点与下划线，供字号/页宽变化和图片加载后调用。
  function repaint() {
    paint()
  }

  function updateStickyAnchors() {
    if (!article.value || !state.data) {
      stickyAnchors.value = {}
      return
    }
    const container = article.value.closest('.note-main') as HTMLElement | null
    if (!container) return
    const containerRect = container.getBoundingClientRect()
    const index = indexArticle(article.value)
    const map: Record<string, { x: number; y: number }> = {}
    for (const item of state.data.stickyNotes) {
      const located = locations.value.get(item.id)
      if (!located) continue
      const domRange = textRange(index, located.start, located.end)
      const rect = domRange?.getClientRects()[0]
      if (!rect) continue
      // 锚到文字行首的右缘，而不是整个段落的中心。
      map[item.id] = {
        x: rect.right - containerRect.left,
        y: rect.top - containerRect.top + rect.height / 2,
      }
    }
    stickyAnchors.value = map
  }

  function paint() {
    const element = article.value
    if (!element) return
    applyMarkdownHighlights(element)
    const data = state.data
    if (!data) return
    // 每个页面只用自己正在显示的正文定位。读文章不会写回或丢弃原始标注。
    const index = indexArticle(element)
    const resolved = new Map<string, TextLocation>()
    for (const item of [...data.highlights, ...data.bookmarks, ...data.stickyNotes]) {
      const range = locate(index, item.quote, item.prefix, item.suffix)
      if (range) resolved.set(item.id, range)
    }
    locations.value = resolved
    applyHighlights(element, data.highlights, resolved)
    applyBookmarks(element, data.bookmarks, resolved)
    updateStickyAnchors()
    applyStickyUnderlines()
    element.querySelectorAll('img').forEach(image => image.addEventListener('load', repaint, { once: true }))
  }

  async function refresh() {
    refreshController?.abort()
    const userID = session.user?.id
    const noteID = note.value?.id
    const element = article.value
    if (!userID || !noteID || !element) {
      state.data = null
      state.loading = false
      paint()
      return
    }
    const controller = new AbortController()
    refreshController = controller
    const isCurrent = () => !controller.signal.aborted
      && session.user?.id === userID && note.value?.id === noteID && article.value === element
    state.loading = true
    try {
      const data = await loadAnnotations(noteID, controller.signal)
      if (!isCurrent()) return
      state.data = data
      state.memoDraft = data.memo?.body || ''
      paint()
    } catch (reason) {
      if (isCurrent() && reason instanceof ApiError && reason.status === 401) state.data = null
    } finally {
      if (refreshController === controller) state.loading = false
    }
  }

  function reset() {
    refreshController?.abort()
    state.loading = false
    state.busy = false
    locations.value = new Map()
    if (article.value) {
      clearHighlights(article.value)
      applyBookmarks(article.value, [], locations.value)
    }
    article.value?.querySelectorAll('.highlight-layer, .markdown-highlight-layer, .sticky-hover-layer, .sticky-underline-layer').forEach(layer => layer.remove())
    hoverLayer = null
    underlineLayer = null
    stickyAnchors.value = {}
    activeRange = null
    state.data = null
    state.open = false
    state.notice = ''
    state.memoDraft = ''
    state.memoStatus = ''
    selection.value = null
  }

  let activeRange: Range | null = null

  function placeSelection(anchor: AnchorInput, rect: DOMRect) {
    if (typeof window === 'undefined') return
    const half = 120
    const height = 32
    const x = Math.min(Math.max(rect.left + rect.width / 2, half + 8), Math.max(half + 8, window.innerWidth - half - 8))
    const y = rect.top >= height + 20
      ? rect.top - height - 6
      : Math.min(rect.bottom + 6, window.innerHeight - height - 6)
    selection.value = { anchor, x, y }
  }

  function positionSelection(anchor: AnchorInput, range: Range) {
    activeRange = range.cloneRange()
    placeSelection(anchor, range.getBoundingClientRect())
  }

  function followSelection() {
    updateStickyAnchors()
    if (!selection.value || !activeRange) return
    const rect = activeRange.getBoundingClientRect()
    if (!rect.width && !rect.height) {
      selection.value = null
      activeRange = null
      return
    }
    placeSelection(selection.value.anchor, rect)
  }

  if (typeof window !== 'undefined') {
    window.addEventListener('scroll', followSelection, { passive: true, capture: true })
    window.addEventListener('resize', followSelection)
  }

  onBeforeUnmount(() => {
    window.removeEventListener('scroll', followSelection, { capture: true })
    window.removeEventListener('resize', followSelection)
  })
  function onMouseUp() {
    if (!session.user || !article.value) return
    const current = window.getSelection()
    if (!current || current.isCollapsed || current.rangeCount === 0) {
      selection.value = null
      return
    }
    const range = current.getRangeAt(0)
    if (!article.value.contains(range.commonAncestorContainer)) {
      selection.value = null
      return
    }
    const anchor = selectionAnchor(article.value, range)
    if (!anchor) {
      selection.value = null
      return
    }
    positionSelection(anchor, range)
    // 滚动/布局刚变化时首帧的 rect 可能还是旧值，下一帧再校正一次。
    requestAnimationFrame(() => {
      if (selection.value?.anchor === anchor) positionSelection(anchor, range)
    })
  }

  async function saveSelection() {
    const data = state.data
    if (!note.value || !data || !selection.value) return
    state.busy = true
    try {
      const created = await createHighlight(
        note.value.id, selection.value.anchor, selectionColor.value, '', article.value?.dataset.sourceRevision || '',
      )
      if (state.data !== data) return
      data.highlights.push(created)
      state.open = true
      state.tab = 'highlights'
      selection.value = null
      window.getSelection()?.removeAllRanges()
      paint()
      notify('已保存划线')
    } catch (reason) {
      if (state.data === data) notify(reason instanceof Error ? reason.message : '保存失败')
    } finally {
      if (state.data === data) state.busy = false
    }
  }

  function cancelSelection() {
    selection.value = null
    activeRange = null
    window.getSelection()?.removeAllRanges()
  }

  async function removeHighlight(target: Highlight) {
    state.busy = true
    try {
      await deleteHighlight(target.id)
      if (state.data) state.data.highlights = state.data.highlights.filter(item => item.id !== target.id)
      paint()
      notify('已删除划线')
    } catch (reason) {
      notify(reason instanceof Error ? reason.message : '删除失败')
    } finally {
      state.busy = false
    }
  }

  async function removeBookmark(target: Bookmark) {
    state.busy = true
    try {
      await deleteBookmark(target.id)
      if (state.data) state.data.bookmarks = state.data.bookmarks.filter(item => item.id !== target.id)
      paint()
      notify('已删除书签')
    } catch (reason) {
      notify(reason instanceof Error ? reason.message : '删除失败')
    } finally {
      state.busy = false
    }
  }

  function scrollToAnchor(id: string) {
    const range = locations.value.get(id)
    if (!article.value || !range) return false
    const element = elementAtOffset(indexArticle(article.value), range.start)
    element?.scrollIntoView({ behavior: 'smooth', block: 'center' })
    return Boolean(element)
  }

  function scrollToHighlight(target: Highlight) {
    if (!article.value) return false
    // 已经渲染出来的划线直接用元素定位，命中率最高，也不受重复文本影响。
    const rendered = article.value.querySelector(`[data-highlight-id="${target.id}"]`)
    if (rendered) {
      rendered.scrollIntoView({ behavior: 'smooth', block: 'center' })
      return true
    }
    return scrollToAnchor(target.id)
  }

  function openHighlight(target: Highlight, scroll = true) {
    state.open = true
    state.tab = 'highlights'
    state.activeHighlight = target.id
    // 点击正文里已经看得见的划线时不再滚动，避免页面突然跳走。
    if (!scroll) return
    if (!scrollToHighlight(target)) {
      notify('这条划线暂时无法定位')
    }
  }

  function onArticleClick(event: MouseEvent) {
    const flag = (event.target as HTMLElement).closest('.bookmark-flag')
    if (flag) {
      state.open = true
      state.tab = 'bookmarks'
      return
    }
    const target = (event.target as HTMLElement).closest('mark[data-highlight-id], img[data-highlight-id]')
    if (!target || !state.data) return
    const highlight = state.data.highlights.find(item => item.id === (target as HTMLElement).dataset.highlightId)
    if (highlight) openHighlight(highlight, false)
  }

  // 取当前视口顶部正在读的段落，而不是文章标题：书签要记录"读到哪"。
  function blockAtViewport() {
    if (!article.value) return null
    const blocks = [...article.value.querySelectorAll<HTMLElement>('p, li, h2, h3, h4, blockquote')]
      .filter(element => (element.textContent || '').trim().length > 0)
    return blocks.find(element => element.getBoundingClientRect().bottom > 180) || blocks[blocks.length - 1] || null
  }

  async function addBookmark() {
    const data = state.data
    if (!note.value || !data || !article.value) return
    const block = blockAtViewport()
    if (!block) return
    state.busy = true
    try {
      const textIndex = indexArticle(article.value)
      const walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT)
      const firstText = walker.nextNode()
      const entry = textIndex.nodes.find(node => node.node === firstText)
      const start = entry?.start || 0
      const headings = [...article.value.querySelectorAll<HTMLElement>('h1,h2,h3,h4')]
      const before = headings.filter(heading => heading.compareDocumentPosition(block) & Node.DOCUMENT_POSITION_FOLLOWING)
      const heading = (before[before.length - 1]?.textContent || headingText(article.value, block) || '阅读位置').trim()
      const top = window.scrollY
      const total = document.documentElement.scrollHeight - window.innerHeight
      const position = total > 0 ? Math.min(1, Math.max(0, top / total)) : 0
      const anchor = anchorForTextRange(textIndex, start, start + 120, heading)
      if (!anchor) return
      const revision = article.value.dataset.sourceRevision || ''
      const existing = data.bookmarks[0]
      if (existing) {
        const updated = await updateBookmark(existing.id, {
          quote: anchor.quote, prefix: anchor.prefix, suffix: anchor.suffix,
          heading: anchor.heading, position, revision,
        })
        if (state.data !== data) return
        Object.assign(existing, updated)
        notify('已更新阅读位置')
      } else {
        const created = await createBookmark(note.value.id, anchor, heading, position, revision)
        if (state.data !== data) return
        data.bookmarks.unshift(created)
        notify('已在当前位置放好书签')
      }
      state.open = true
      state.tab = 'bookmarks'
      paint()
    } catch (reason) {
      if (state.data === data) notify(reason instanceof Error ? reason.message : '添加失败')
    } finally {
      if (state.data === data) state.busy = false
    }
  }

  function jumpToBookmark(target: Bookmark) {
    const flag = article.value?.querySelector<HTMLElement>(`.bookmark-flag[data-bookmark-id="${target.id}"]`)
    if (flag) {
      flag.scrollIntoView({ behavior: 'smooth', block: 'center' })
      return
    }
    if (scrollToAnchor(target.id)) return
    const total = document.documentElement.scrollHeight - window.innerHeight
    window.scrollTo({ top: target.position * total, behavior: 'smooth' })
  }

  function scheduleTextSave() {
    const noteID = note.value?.id
    const userID = session.user?.id
    if (!noteID || !userID) return
    const body = state.memoDraft
    clearTimeout(memoTimer)
    state.memoStatus = '正在输入…'
    memoTimer = setTimeout(async () => {
      if (session.user?.id !== userID) return
      const isCurrent = () => session.user?.id === userID && note.value?.id === noteID && state.memoDraft === body
      try {
        const memo = await saveMemo(noteID, body)
        if (!isCurrent()) return
        if (state.data) state.data.memo = memo
        state.memoStatus = '已自动保存'
      } catch (reason) {
        if (isCurrent()) state.memoStatus = reason instanceof Error ? reason.message : '保存失败'
      }
    }, 700)
  }


  const stickyTimers = new Map<string, ReturnType<typeof setTimeout>>()

  async function addSticky(anchor?: AnchorInput, body = '新便条') {
    if (!session.user) {
      notify('登录后才能新建便条')
      return
    }
    if (!note.value) return
    const data = state.data
    if (!data) {
      notify('标注还在加载，请稍后再试')
      return
    }
    state.busy = true
    try {
      const created = await createStickyNote(note.value.id, body, 'yellow', anchor, article.value?.dataset.sourceRevision || '')
      if (state.data !== data) return
      data.stickyNotes.push(created)
      paint()
      notify('已新建便条')
      if (!stickyAnchors.value[created.id]) {
        openDrawer('sticky')
        await nextTick()
        document.querySelector<HTMLTextAreaElement>('.annotation-drawer .annotation-item:last-child textarea')?.focus()
        return
      }
      await nextTick()
      const cards = document.querySelectorAll<HTMLTextAreaElement>('.sticky-float textarea')
      const target = cards[cards.length - 1]
      target?.focus()
      target?.closest('.sticky-float')?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    } catch (reason) {
      if (state.data === data) notify(reason instanceof Error ? reason.message : '新建便条失败')
    } finally {
      if (state.data === data) state.busy = false
    }
  }

  function setStickyBody(item: StickyNote, body: string) {
    item.body = body
    scheduleStickySave(item)
  }

  function scheduleStickySave(item: StickyNote) {
    const userID = session.user?.id
    const noteID = note.value?.id
    const body = item.body
    if (!userID || !noteID) return
    clearTimeout(stickyTimers.get(item.id))
    stickyTimers.set(item.id, setTimeout(async () => {
      stickyTimers.delete(item.id)
      if (session.user?.id !== userID || !body.trim()) return
      try {
        const updated = await updateStickyNote(item.id, { body })
        if (session.user?.id === userID && item.body === body) item.body = updated.body
      } catch (reason) {
        if (session.user?.id === userID && note.value?.id === noteID) {
          notify(reason instanceof Error ? reason.message : '便条保存失败')
        }
      }
    }, 700))
  }

  async function recolorSticky(item: StickyNote, color: HighlightColor) {
    try {
      const updated = await updateStickyNote(item.id, { color })
      item.color = updated.color
      applyStickyUnderlines()
    } catch (reason) {
      notify(reason instanceof Error ? reason.message : '颜色保存失败')
    }
  }

  async function removeSticky(item: StickyNote) {
    try {
      await deleteStickyNote(item.id)
      if (state.data) state.data.stickyNotes = state.data.stickyNotes.filter(note => note.id !== item.id)
      paint()
      notify('已删除便条')
    } catch (reason) {
      notify(reason instanceof Error ? reason.message : '删除失败')
    }
  }

  let hoverLayer: HTMLElement | null = null

  function jumpToSticky(item: StickyNote) {
    if (scrollToAnchor(item.id)) closeDrawer()
  }

  // 悬浮便条时，临时高亮它锚定的那段正文；移开即移除，不干扰正常阅读。
  function hoverSticky(item: StickyNote, active: boolean) {
    hoverLayer?.remove()
    hoverLayer = null
    if (!active || !article.value || !state.data) return
    const index = indexArticle(article.value)
    const located = locations.value.get(item.id)
    if (!located) return
    const range = textRange(index, located.start, located.end)
    if (!range) return
    const articleRect = article.value.getBoundingClientRect()
    const layer = document.createElement('div')
    layer.className = 'sticky-hover-layer'
    layer.setAttribute('aria-hidden', 'true')
    for (const rect of range.getClientRects()) {
      if (rect.width < 1 || rect.height < 1) continue
      const band = document.createElement('div')
      band.className = `sticky-hover-band hover-${item.color}`
      band.style.left = `${rect.left - articleRect.left}px`
      band.style.top = `${rect.top - articleRect.top}px`
      band.style.width = `${rect.width}px`
      band.style.height = `${rect.height}px`
      layer.appendChild(band)
    }
    article.value.prepend(layer)
    hoverLayer = layer
  }

  let underlineLayer: HTMLElement | null = null

  // 每条便条在锚定文字下方画一条常驻的彩色下划线。
  function applyStickyUnderlines() {
    underlineLayer?.remove()
    underlineLayer = null
    if (!article.value || !state.data?.stickyNotes.length) return
    const index = indexArticle(article.value)
    const articleRect = article.value.getBoundingClientRect()
    const layer = document.createElement('div')
    layer.className = 'sticky-underline-layer'
    layer.setAttribute('aria-hidden', 'true')
    for (const item of state.data.stickyNotes) {
      const located = locations.value.get(item.id)
      if (!located) continue
      const range = textRange(index, located.start, located.end)
      if (!range) continue
      for (const rect of range.getClientRects()) {
        if (rect.width < 1) continue
        const line = document.createElement('div')
        line.className = `sticky-underline underline-${item.color}`
        line.style.left = `${rect.left - articleRect.left}px`
        line.style.top = `${rect.bottom - articleRect.top - 2}px`
        line.style.width = `${rect.width}px`
        layer.appendChild(line)
      }
    }
    article.value.prepend(layer)
    underlineLayer = layer
  }
  function openDrawer(tab: AnnotationTab = 'highlights') {
    state.open = true
    state.tab = tab
  }

  function closeDrawer() {
    state.open = false
  }

  watch(() => session.user?.id, id => {
    reset()
    if (id) void refresh()
  })

  return {
    state, selection, selectionColor, locations, stickyAnchors, floatingStickyNotes, loggedIn, repaint,
    refresh, reset, onMouseUp, saveSelection, cancelSelection, removeHighlight,
    removeBookmark, openHighlight, onArticleClick, addBookmark, jumpToBookmark, scheduleTextSave,
    addSticky, setStickyBody, scheduleStickySave, recolorSticky, removeSticky, hoverSticky, jumpToSticky,
    openDrawer, closeDrawer,
  }
}
