<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
import { trackNoteView } from '@/lib/analytics'
import { categoryName, coursePath, getNote, notePath, notesForCourse } from '@/lib/content'
import { useReadingProgress } from '@/lib/progress'
import { useRecentNotes } from '@/lib/recent'
import { useFavorites } from '@/lib/favorites'
import { session } from '@/lib/session'
import NoteTocCard from '@/components/NoteTocCard.vue'
import StickyNoteCard from '@/components/StickyNoteCard.vue'
import ContextMenu, { type MenuItem } from '@/components/ContextMenu.vue'
import type { StickyNote } from '@/lib/annotations'
import { fetchArticle, takeInitialArticle } from '@/lib/initial-article'
import { useNoteAnnotations } from '@/lib/useNoteAnnotations'
import { readingSettings, resetReadingSettings, updateReadingSettings } from '@/lib/readingSettings'

const route = useRoute()
// 正文与版本一起加载，标注使用读者实际看到的版本。
const body = ref(takeInitialArticle())
const loading = ref(!body.value)
const loadError = ref('')
const readingProgress = ref(0)
const reading = readingSettings
const fontScale = computed(() => readingSettings.fontScale)
const copied = ref(false)
const activeHeading = ref('')
const zoomedImage = ref<{ src: string; alt: string }>()
const article = ref<HTMLElement>()
const mobileToc = ref<HTMLDetailsElement>()
const readingDetails = ref<HTMLDetailsElement>()
let observer: IntersectionObserver | undefined

function parameter(value: unknown) {
  const text = String(Array.isArray(value) ? value[0] : value || '')
  try { return decodeURIComponent(text) } catch (_) { return text }
}

const note = computed(() => getNote(parameter(route.params.slug)))
const id = computed(() => note.value?.id || '')
const annotations = useNoteAnnotations(article, note, computed(() => body.value?.revision))
const selection = annotations.selection
const selectionColor = annotations.selectionColor
const { locations, stickyAnchors, floatingStickyNotes } = annotations
const annotationCount = computed(() => {
  const data = annotations.state.data
  return data ? data.highlights.length + data.stickyNotes.length : 0
})
const contextMenu = ref<{ x: number; y: number; items: MenuItem[] } | null>(null)

function handleArticleClick(event: MouseEvent) {
  zoomImage(event)
  annotations.onArticleClick(event)
}

// 右键菜单按点击目标给不同操作：划线、便条、书签、删除。
function openContextMenu(event: MouseEvent) {
  if (!annotations.loggedIn.value) return
  const target = event.target as HTMLElement
  const x = Math.min(event.clientX, window.innerWidth - 210)
  const y = Math.min(event.clientY, window.innerHeight - 280)
  const items: MenuItem[] = []
  const mark = target.closest('mark[data-highlight-id], span.math-mark[data-highlight-id]')
  const flag = target.closest('.bookmark-flag')
  if (mark) {
    const highlight = annotations.state.data?.highlights.find(item => item.id === (mark as HTMLElement).dataset.highlightId)
    if (highlight) {
      items.push({ label: '删除划线', danger: true, action: () => annotations.removeHighlight(highlight) })
    }
  } else if (flag) {
    items.push({ label: '打开书签列表', action: () => annotations.openDrawer('bookmarks') })
    const bookmark = annotations.state.data?.bookmarks.find(item => item.id === (flag as HTMLElement).dataset.bookmarkId)
    if (bookmark) items.push({ label: '删除书签', danger: true, action: () => annotations.removeBookmark(bookmark) })
  } else {
    const selected = window.getSelection()?.toString().trim()
    if (selected) {
      annotations.onMouseUp()
      const colors = [['yellow', '黄色'], ['green', '绿色'], ['blue', '蓝色'], ['pink', '粉色']] as const
      for (const [color, name] of colors) {
        items.push({
          label: `${name}划线`,
          action: () => {
            annotations.selectionColor.value = color
            annotations.saveSelection()
          },
        })
      }
      items.push({ label: '新建便条', action: () => annotations.addSticky(annotations.selection.value?.anchor) })
    } else {
      items.push({ label: '添加阅读书签', action: () => annotations.addBookmark() })
    }
  }
  if (!items.length) return
  event.preventDefault()
  contextMenu.value = { x, y, items }
}

const courseNotes = computed(() => note.value ? notesForCourse(note.value.category, note.value.course) : [])
const currentIndex = computed(() => courseNotes.value.findIndex(item => item.id === id.value))
const previous = computed(() => currentIndex.value > 0 ? courseNotes.value[currentIndex.value - 1] : undefined)
const next = computed(() => currentIndex.value >= 0 ? courseNotes.value[currentIndex.value + 1] : undefined)
const activeHeadingTitle = computed(() => note.value?.headings.find(heading => heading.slug === activeHeading.value)?.title)
const { completed, toggleCompleted, markRead } = useReadingProgress()
const { isFavorite, toggleFavorite } = useFavorites()
const { markRecent } = useRecentNotes()
useHead({ title: () => `${note.value?.title || '笔记未找到'} · 碧山秋云`, meta: [{ name: 'description', content: () => note.value?.excerpt || '' }] })

function updateProgress() {
  const height = document.documentElement.scrollHeight - window.innerHeight
  readingProgress.value = height > 0 ? Math.min(100, Math.max(0, window.scrollY / height * 100)) : 0
}

function observeHeadings() {
  observer?.disconnect()
  if (!article.value) return
  observer = new IntersectionObserver(entries => {
    const visible = entries.filter(entry => entry.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
    if (visible[0]) activeHeading.value = visible[0].target.id
  }, { rootMargin: '-90px 0px -70% 0px', threshold: 0 })
  article.value.querySelectorAll('h1,h2,h3').forEach(heading => observer?.observe(heading))
}

async function copyLink() {
  await navigator.clipboard.writeText(window.location.href)
  copied.value = true
  setTimeout(() => { copied.value = false }, 1600)
}

function changeFont(direction: number) {
  updateReadingSettings({ fontScale: Math.min(1.35, Math.max(0.85, Math.round((fontScale.value + direction * 0.05) * 100) / 100)) })
}

// 公式只作为正文排版的一部分，不提供点击放大；正文里真正的插图才放大查看。
function zoomImage(event: MouseEvent) {
  const target = (event.target as HTMLElement).closest('img')
  if (!target || target.classList.contains('math-image')) return
  if (target.closest('a')) return
  const image = target as HTMLImageElement
  zoomedImage.value = { src: image.currentSrc || image.src, alt: image.alt }
}

function keyboard(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (zoomedImage.value) zoomedImage.value = undefined
  else if (readingDetails.value?.open) readingDetails.value.open = false
}

// 点击阅读设置面板之外的区域时收起，不强制再点一次按钮。
function closeReadingOnOutside(event: PointerEvent) {
  const details = readingDetails.value
  if (!details?.open) return
  if (!details.contains(event.target as Node)) details.open = false
}
function scrollTop() { window.scrollTo({ top: 0, behavior: 'smooth' }) }

onMounted(() => {
  watch(id, async (_id, previousID, onCleanup) => {
    const current = note.value
    if (previousID !== undefined) {
      annotations.reset()
      body.value = undefined
      activeHeading.value = ''
      loadError.value = ''
    }
    if (!current) {
      loading.value = false
      return
    }
    markRecent(current.id)
    trackNoteView(current.id)
    markRead(current.id)
    if (body.value) return

    const controller = new AbortController()
    onCleanup(() => controller.abort())
    loading.value = true
    try {
      const result = await fetchArticle(current.slug, controller.signal)
      if (!controller.signal.aborted) body.value = result
    } catch (reason) {
      if (controller.signal.aborted) return
      loadError.value = reason instanceof Error ? reason.message : '笔记加载失败'
    } finally {
      if (!controller.signal.aborted) loading.value = false
    }
  }, { immediate: true })

  window.addEventListener('scroll', updateProgress, { passive: true })
  window.addEventListener('resize', annotations.repaint)
  window.addEventListener('pointerdown', closeReadingOnOutside, true)
  window.addEventListener('keydown', keyboard)
  updateProgress()
  watch([article, () => body.value?.revision], ([element]) => {
    observeHeadings()
    updateProgress()
    if (element) void annotations.refresh()
  }, { immediate: true, flush: 'post' })
})
watch(reading, async () => { await nextTick(); annotations.repaint() }, { deep: true })
onBeforeUnmount(() => {
  annotations.reset()
  window.removeEventListener('scroll', updateProgress)
  window.removeEventListener('resize', annotations.repaint)
  window.removeEventListener('pointerdown', closeReadingOnOutside, true)
  window.removeEventListener('keydown', keyboard)
  observer?.disconnect()
})
</script>

<template>
  <div v-if="note" class="note-page" :style="{ '--article-scale': reading.fontScale, '--reading-width': `${reading.width}px`, '--article-line-height': reading.lineHeight, '--article-font': reading.font === 'sans' ? 'var(--sans, system-ui)' : 'var(--serif)' }">
    <div class="reading-progress" :style="{ width: `${readingProgress}%` }" />
    <div class="page-shell note-shell">
      <nav class="breadcrumbs" aria-label="面包屑">
        <RouterLink to="/library">笔记库</RouterLink><span>/</span>
        <RouterLink :to="`/library/${note.category}`">{{ categoryName(note.category, note.categoryTitle) }}</RouterLink><span>/</span>
        <RouterLink :to="coursePath(note)">{{ note.course }}</RouterLink><span>/</span>
        <strong>{{ note.title }}</strong>
      </nav>
      <div class="note-layout">
        <main class="note-main">
          <header class="note-header">
            <h1>{{ note.title }}</h1>
            <div class="note-meta">
              <span><AppIcon name="clock" :size="15" />约 {{ note.readingMinutes }} 分钟</span>
              <span><AppIcon name="file" :size="15" />{{ note.characters }} 字</span>
              <span>第 {{ currentIndex + 1 }} 篇，共 {{ courseNotes.length }} 篇</span>
            </div>
            <div class="note-actions" aria-label="阅读操作">
              <button :class="{ completed: completed.has(note.id) }" @click="toggleCompleted(note.id)">
                <AppIcon :name="completed.has(note.id) ? 'checkCircle' : 'check'" :size="16" />
                {{ completed.has(note.id) ? '已读' : '标记已读' }}
              </button>
              <div class="font-stepper" aria-label="正文字号">
                <button :disabled="fontScale <= 0.85" aria-label="减小字号" @click="changeFont(-1)">A−</button>
                <span>{{ Math.round(fontScale * 100) }}%</span>
                <button :disabled="fontScale >= 1.35" aria-label="增大字号" @click="changeFont(1)">A+</button>
              </div>
              <details ref="readingDetails" class="reading-settings">
                <summary><AppIcon name="settings" :size="15" />阅读设置</summary>
                <div class="reading-settings-body">
                  <label><span>字号</span><input type="range" min="0.85" max="1.35" step="0.05" :value="reading.fontScale" @input="updateReadingSettings({ fontScale: ($event.currentTarget as HTMLInputElement).valueAsNumber })"><small>{{ Math.round(reading.fontScale * 100) }}%</small></label>
                  <label><span>页宽</span><select :value="reading.width" @change="updateReadingSettings({ width: Number(($event.currentTarget as HTMLSelectElement).value) })"><option :value="640">窄</option><option :value="760">标准</option><option :value="900">宽</option><option :value="980">超宽</option></select></label>
                  <label><span>行距</span><select :value="reading.lineHeight" @change="updateReadingSettings({ lineHeight: Number(($event.currentTarget as HTMLSelectElement).value) })"><option :value="1.7">紧凑</option><option :value="2">标准</option><option :value="2.3">宽松</option></select></label>
                  <label><span>字体</span><select :value="reading.font" @change="updateReadingSettings({ font: ($event.currentTarget as HTMLSelectElement).value === 'sans' ? 'sans' : 'serif' })"><option value="serif">衬线</option><option value="sans">无衬线</option></select></label>
                  <button class="text-link" @click="resetReadingSettings()">恢复默认</button>
                </div>
              </details>
              <button :class="{ completed: isFavorite(note.id) }" :aria-pressed="isFavorite(note.id)" @click="toggleFavorite(note.id)"><AppIcon name="heart" :size="16" />{{ isFavorite(note.id) ? '已收藏' : '收藏' }}</button>
              <button v-if="session.user" title="记录当前阅读位置" @click="annotations.addBookmark()"><AppIcon name="bookmark" :size="16" />书签</button>
              <button v-if="session.user" title="划线、批注与便条" @click="annotations.openDrawer()"><AppIcon name="highlight" :size="16" />标注<span v-if="annotationCount" class="count-badge">{{ annotationCount }}</span></button>
              <button @click="copyLink"><AppIcon :name="copied ? 'check' : 'external'" :size="16" />{{ copied ? '已复制' : '分享' }}</button>
            </div>
          </header>

          <details ref="mobileToc" class="mobile-toc">
            <summary><span>本文大纲</span><small>{{ activeHeadingTitle || `${note.headings.length} 个章节` }}</small><AppIcon name="chevronDown" :size="16" /></summary>
            <nav @click="mobileToc?.removeAttribute('open')">
              <a v-for="heading in note.headings" :key="heading.slug" :href="`#${heading.slug}`" :class="[`level-${heading.level}`, { active: activeHeading === heading.slug }]">{{ heading.title }}</a>
            </nav>
          </details>

          <p v-if="loading" class="search-status"><span class="loading-spinner" />正在展开笔记…</p>
          <div v-else-if="loadError" class="error-message">{{ loadError }}</div>
          <article v-else ref="article" class="note-article" :data-annotation-revision="body?.revision" v-html="body?.html" @click="handleArticleClick" @mouseup="annotations.onMouseUp" @contextmenu.prevent="openContextMenu" />

          <section v-if="!loading && !loadError" class="note-finish">
            <div><strong>{{ completed.has(note.id) ? '这篇笔记已完成' : '读完这篇笔记了吗？' }}</strong><small>{{ session.user ? '已读状态会同步到你的账号。' : '已读状态保存在本机，登录后可跨设备同步。' }}</small></div>
            <button class="button" :class="completed.has(note.id) ? 'button-quiet' : 'button-primary'" @click="toggleCompleted(note.id)">
              <AppIcon :name="completed.has(note.id) ? 'checkCircle' : 'check'" :size="17" />
              {{ completed.has(note.id) ? '取消已读' : '标记为已读' }}
            </button>
          </section>
          <nav v-if="!loading" class="note-pagination" aria-label="课程前后篇">
            <RouterLink v-if="previous" :to="notePath(previous)" class="previous"><AppIcon name="arrowLeft" /><span><small>上一篇</small><strong>{{ previous.title }}</strong></span></RouterLink>
            <span v-else />
            <RouterLink v-if="next" :to="notePath(next)" class="next"><span><small>下一篇</small><strong>{{ next.title }}</strong></span><AppIcon name="arrowRight" /></RouterLink>
          </nav>
          <div class="sticky-layer">
          <StickyNoteCard
            v-for="(item, index) in floatingStickyNotes"
            :key="item.id"
            :item="item"
            :index="index"
            :anchor="stickyAnchors[item.id]"
            @update="annotations.setStickyBody(item, $event)"
            @recolor="annotations.recolorSticky(item, $event)"
            @remove="annotations.removeSticky(item)"
            @hover="annotations.hoverSticky(item, $event)"
          />
        </div>

      </main>
      </div>
    </div>

    <!-- 目录卡片脱离两列 grid，固定定位浮动，避免挤偏正文 -->
    <NoteTocCard :headings="note.headings" :active-slug="activeHeading" />



    <button v-if="session.user && readingProgress > 4" class="reading-bookmark icon-button" :class="{ active: annotations.state.data?.bookmarks.length }" title="记录当前位置为书签" aria-label="记录阅读位置" @click="annotations.addBookmark()"><AppIcon name="bookmark" :size="18" /></button>
<button v-if="readingProgress > 8" class="back-to-top icon-button" aria-label="回到顶部" @click="scrollTop"><AppIcon name="arrowUp" /></button>

    <Teleport to="body">
      <div v-if="zoomedImage" class="image-zoom" role="dialog" aria-modal="true" aria-label="查看图片" @click.self="zoomedImage = undefined">
        <button class="icon-button image-zoom-close" aria-label="关闭图片" @click="zoomedImage = undefined"><AppIcon name="close" /></button>
        <img :src="zoomedImage.src" :alt="zoomedImage.alt"><p>按 Esc 或点击空白处关闭</p>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="selection" class="selection-toolbar" :style="{ left: `${selection.x}px`, top: `${selection.y}px` }" @pointerdown.stop>
        <div class="selection-colors" role="group" aria-label="高亮颜色">
          <button v-for="color in ['yellow', 'green', 'blue', 'pink']" :key="color" :class="['color-dot', color, { active: selectionColor === color }]" :aria-label="`高亮颜色 ${color}`" @click="selectionColor = color" />
        </div>
        <button class="button button-primary selection-action" :disabled="annotations.state.busy" @click="annotations.saveSelection()">划线</button>
        <button class="button button-quiet selection-action" @click="annotations.addSticky(selection?.anchor); annotations.cancelSelection()">便条</button>
        <button class="icon-button selection-close" aria-label="取消" @click="annotations.cancelSelection()"><AppIcon name="close" :size="13" /></button>
      </div>

      <div v-if="annotations.state.open" class="drawer-backdrop" @click.self="annotations.closeDrawer()">
        <aside class="annotation-drawer" role="dialog" aria-modal="true" aria-label="我的标注">
          <header class="drawer-header">
            <div><small>{{ note && categoryName(note.category, note.categoryTitle) }} · {{ note?.course }}</small><h2>我的标注</h2></div>
            <button class="icon-button" aria-label="关闭标注" @click="annotations.closeDrawer()"><AppIcon name="close" /></button>
          </header>
          <nav class="annotation-tabs" role="tablist">
            <button role="tab" :aria-selected="annotations.state.tab === 'highlights'" :class="{ active: annotations.state.tab === 'highlights' }" @click="annotations.state.tab = 'highlights'">划线 {{ annotations.state.data?.highlights.length || 0 }}</button>
            <button role="tab" :aria-selected="annotations.state.tab === 'bookmarks'" :class="{ active: annotations.state.tab === 'bookmarks' }" @click="annotations.state.tab = 'bookmarks'">书签 {{ annotations.state.data?.bookmarks.length || 0 }}</button>
            <button role="tab" :aria-selected="annotations.state.tab === 'sticky'" :class="{ active: annotations.state.tab === 'sticky' }" @click="annotations.state.tab = 'sticky'">便条 {{ annotations.state.data?.stickyNotes.length || 0 }}</button>
          </nav>

          <div v-if="annotations.state.loading" class="drawer-empty"><span class="loading-spinner" />正在读取标注…</div>

          <div v-else-if="annotations.state.tab === 'highlights'" class="annotation-list">
            <p v-if="!annotations.state.data?.highlights.length" class="drawer-empty">在正文里选中文字后，可通过工具栏或右键划线。</p>
            <article v-for="item in annotations.state.data?.highlights" :key="item.id" :class="['annotation-item', { active: annotations.state.activeHighlight === item.id }]">
              <p class="annotation-quote"><span :class="['color-dot', item.color]" aria-hidden="true" />“{{ item.quote }}”</p>
              <p v-if="item.comment" class="annotation-quote">{{ item.comment }}</p>
              <footer>
                <span v-if="!locations.has(item.id)" class="annotation-badge">暂时无法定位</span>
                <button v-else class="text-link" @click="annotations.openHighlight(item)">定位</button>
                <button class="text-link danger" @click="annotations.removeHighlight(item)">删除</button>
              </footer>
            </article>
          </div>

          <div v-else-if="annotations.state.tab === 'bookmarks'" class="annotation-list">
            <p v-if="!annotations.state.data?.bookmarks.length" class="drawer-empty">点击正文上方的「书签」记录当前位置。</p>
            <article v-for="item in annotations.state.data?.bookmarks" :key="item.id" class="annotation-item bookmark-item">
              <button class="bookmark-main" @click="annotations.jumpToBookmark(item)">
                <AppIcon name="bookmark" :size="16" />
                <span><strong>{{ item.label || item.heading || '阅读位置' }}</strong><small>{{ item.quote || '按滚动位置定位' }}</small></span>
              </button>
              <footer>
                <span v-if="item.quote && !locations.has(item.id)" class="annotation-badge">按原阅读进度跳转</span>
                <button class="text-link danger" @click="annotations.removeBookmark(item)">删除</button>
              </footer>
            </article>
          </div>

          <div v-else-if="annotations.state.tab === 'sticky'" class="annotation-list">
            <p v-if="!annotations.state.data?.stickyNotes.length" class="drawer-empty">选中文字后点击「便条」，可以把便条贴到正文旁。</p>
            <article v-for="item in annotations.state.data?.stickyNotes" :key="item.id" class="annotation-item">
              <p v-if="item.quote" class="annotation-quote">“{{ item.quote }}”</p>
              <textarea :value="item.body" rows="3" aria-label="便条内容" @input="annotations.setStickyBody(item, ($event.currentTarget as HTMLTextAreaElement).value)" />
              <footer>
                <button v-if="locations.has(item.id)" class="text-link" @click="annotations.jumpToSticky(item)">定位</button>
                <span v-else-if="item.quote" class="annotation-badge">暂时无法定位</span>
                <div class="sticky-colors">
                  <button v-for="color in (['yellow', 'green', 'blue', 'pink'] as const)" :key="color" :class="['color-dot', color, { active: item.color === color }]" :aria-label="`便条颜色 ${color}`" :aria-pressed="item.color === color" @click="annotations.recolorSticky(item, color)" />
                </div>
                <button class="text-link danger" @click="annotations.removeSticky(item)">删除</button>
              </footer>
            </article>
          </div>
        </aside>
      </div>

      <p v-if="annotations.state.notice" class="annotation-toast" role="status">{{ annotations.state.notice }}</p>
    </Teleport>

    <ContextMenu v-if="contextMenu" :x="contextMenu.x" :y="contextMenu.y" :items="contextMenu.items" @close="contextMenu = null" />
  </div>
  <div v-else class="empty-state page-shell"><AppIcon name="file" :size="38" /><h1>没有找到这篇笔记</h1><p>它可能尚未发布，或者链接已经改变。</p><RouterLink to="/library" class="button button-primary">返回笔记库</RouterLink></div>
</template>
