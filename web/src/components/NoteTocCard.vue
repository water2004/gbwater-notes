<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { headingSlug } from '@/lib/slug'
import type { Heading } from '@/types/content'

const props = defineProps<{ headings: Heading[]; activeSlug?: string }>()

interface TocNode {
  key: string
  title: string
  slug: string
  level: number
  children: TocNode[]
}

interface TocRow {
  node: TocNode
  depth: number
}

const STORAGE_KEY = 'gbwater-toc-card'
const MARGIN = 18
const WIDTH = 232
const PILL_WIDTH = 88
const PILL_HEIGHT = 36

const collapsed = ref(false)
const x = ref(0)
const y = ref(140)
const height = ref<number | null>(null)
const expandedHeight = ref(0)
const dragging = ref(false)
const openKeys = ref<Set<string>>(new Set())
const cardRef = ref<HTMLElement>()

// 按标题层级建树：h2 是章节、h3 是子章节。
const tree = computed<TocNode[]>(() => {
  const roots: TocNode[] = []
  const stack: TocNode[] = []
  props.headings.forEach((heading, index) => {
    const node: TocNode = {
      key: `h-${index}`,
      title: heading.title,
      slug: headingSlug(heading.title),
      level: heading.level,
      children: [],
    }
    while (stack.length && stack[stack.length - 1].level >= node.level) stack.pop()
    if (stack.length) stack[stack.length - 1].children.push(node)
    else roots.push(node)
    stack.push(node)
  })
  return roots
})

function findPath(nodes: TocNode[], slug: string, trail: string[] = []): string[] | null {
  for (const node of nodes) {
    const next = [...trail, node.key]
    if (node.slug === slug) return next
    const found = findPath(node.children, slug, next)
    if (found) return found
  }
  return null
}

const rows = computed<TocRow[]>(() => {
  const result: TocRow[] = []
  const walk = (nodes: TocNode[], depth: number) => {
    for (const node of nodes) {
      result.push({ node, depth })
      if (openKeys.value.has(node.key)) walk(node.children, depth + 1)
    }
  }
  walk(tree.value, 0)
  return result
})

// 滚动进入新章节时自动展开该分支、收起其它分支；首次进入默认展开第一节。
watch(() => props.activeSlug, slug => {
  if (!slug) {
    const first = tree.value[0]
    if (first) openKeys.value = new Set([first.key])
    return
  }
  const path = findPath(tree.value, slug)
  if (path) openKeys.value = new Set(path)
}, { immediate: true })

function toggle(node: TocNode, event: MouseEvent) {
  event.preventDefault()
  event.stopPropagation()
  const next = new Set(openKeys.value)
  if (next.has(node.key)) next.delete(node.key)
  else next.add(node.key)
  openKeys.value = next
}

function edgeFor(width: number) {
  return Math.max(MARGIN, (typeof window !== 'undefined' ? window.innerWidth : 1280) - width - MARGIN)
}

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

function clampY(value: number) {
  const max = typeof window !== 'undefined' ? window.innerHeight - 80 : 600
  return clamp(value, 92, max)
}

function measureExpanded() {
  if (collapsed.value || !cardRef.value) return
  const value = cardRef.value.offsetHeight
  if (value > 0) expandedHeight.value = value
}

function load() {
  if (typeof localStorage === 'undefined') return
  try {
    const saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || '{}')
    collapsed.value = Boolean(saved.collapsed)
    const width = collapsed.value ? PILL_WIDTH : WIDTH
    x.value = typeof saved.x === 'number' ? clamp(saved.x, MARGIN, edgeFor(width)) : edgeFor(width)
    y.value = clampY(typeof saved.y === 'number' ? saved.y : 140)
    if (collapsed.value) height.value = PILL_HEIGHT
  } catch (_) {
    x.value = edgeFor(WIDTH)
  }
}

function persist() {
  if (typeof localStorage === 'undefined') return
  localStorage.setItem(STORAGE_KEY, JSON.stringify({ x: x.value, y: y.value, collapsed: collapsed.value }))
}

function activeElement() {
  return cardRef.value
}

// 拖拽时直接写 DOM，避免渲染调度让内容滞后于卡片。
function moveTo(nextX: number, nextY: number) {
  x.value = nextX
  y.value = nextY
  const element = activeElement()
  if (element) {
    element.style.left = `${nextX}px`
    element.style.top = `${nextY}px`
  }
}

let drag: { startX: number; startY: number; originX: number; originY: number; moved: boolean } | null = null
let suppressClick = false

function startDrag(event: PointerEvent) {
  if (event.button !== 0) return
  if ((event.target as HTMLElement).closest('button') && !collapsed.value) return
  event.preventDefault()
  drag = { startX: event.clientX, startY: event.clientY, originX: x.value, originY: y.value, moved: false }
  dragging.value = true
  const element = activeElement()
  if (element) element.style.transition = 'none'
  window.addEventListener('pointermove', onDrag)
  window.addEventListener('pointerup', stopDrag, { once: true })
}

function onDrag(event: PointerEvent) {
  if (!drag) return
  const dx = event.clientX - drag.startX
  const dy = event.clientY - drag.startY
  if (Math.abs(dx) + Math.abs(dy) > 5) drag.moved = true
  moveTo(clamp(drag.originX + dx, MARGIN, edgeFor(collapsed.value ? PILL_WIDTH : WIDTH)), clampY(drag.originY + dy))
}

// 松手后恢复过渡，只更新状态，让 left/top 自身做吸附动画。
function stopDrag() {
  window.removeEventListener('pointermove', onDrag)
  const moved = drag?.moved || false
  drag = null
  dragging.value = false
  const element = activeElement()
  if (element) element.style.transition = ''
  x.value = x.value + (collapsed.value ? PILL_WIDTH : WIDTH) / 2 < window.innerWidth / 2
    ? MARGIN
    : edgeFor(collapsed.value ? PILL_WIDTH : WIDTH)
  persist()
  if (moved) {
    suppressClick = true
    setTimeout(() => { suppressClick = false }, 0)
  }
}

// 最小化：先把卡片高度锁成当前像素值，再收缩到胶囊尺寸，形成连续形变。
async function minimize() {
  const element = cardRef.value
  if (!element) return
  expandedHeight.value = element.offsetHeight
  height.value = expandedHeight.value
  await nextTick()
  collapsed.value = true
  height.value = PILL_HEIGHT
  x.value = edgeFor(PILL_WIDTH)
  persist()
}

// 展开：从胶囊尺寸连续长回卡片，动画结束后把高度交还给内容。
async function expand() {
  if (suppressClick) return
  collapsed.value = false
  height.value = PILL_HEIGHT
  x.value = edgeFor(WIDTH)
  persist()
  await nextTick()
  height.value = expandedHeight.value || 340
}

function onTransitionEnd(event: TransitionEvent) {
  if (event.target !== cardRef.value || event.propertyName !== 'height') return
  if (!collapsed.value) height.value = null
}

function onResize() {
  x.value = clamp(x.value, MARGIN, edgeFor(collapsed.value ? PILL_WIDTH : WIDTH))
  y.value = clampY(y.value)
}

const positionStyle = computed(() => {
  const style: Record<string, string> = {
    left: `${x.value}px`,
    top: `${y.value}px`,
    width: `${collapsed.value ? PILL_WIDTH : WIDTH}px`,
  }
  if (height.value !== null) style.height = `${height.value}px`
  return style
})

watch(rows, async () => {
  await nextTick()
  measureExpanded()
}, { flush: 'post' })

onMounted(() => {
  load()
  nextTick(measureExpanded)
  window.addEventListener('resize', onResize)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', onResize)
  window.removeEventListener('pointermove', onDrag)
})
</script>

<template>
  <div
    ref="cardRef"
    class="toc-card"
    :class="{ dragging, collapsed }"
    :style="positionStyle"
    :role="collapsed ? 'button' : undefined"
    :aria-label="collapsed ? '展开目录' : '文章目录'"
    @pointerdown="collapsed ? startDrag($event) : undefined"
    @click="collapsed ? expand() : undefined"
    @transitionend="onTransitionEnd"
  >
    <header class="toc-card-head" @pointerdown="startDrag">
      <span class="toc-grip" aria-hidden="true"><AppIcon name="menu" :size="14" /></span>
      <strong>本文目录</strong>
      <button class="icon-button" aria-label="最小化目录" @click.stop="minimize"><AppIcon name="minimize" :size="14" /></button>
    </header>
    <nav class="toc-card-body">
      <TransitionGroup name="toc-row" tag="div" class="toc-rows">
        <div v-for="row in rows" :key="row.node.key" class="toc-row" :class="{ active: row.node.slug === activeSlug }" :style="{ paddingLeft: `${6 + row.depth * 13}px` }">
          <button v-if="row.node.children.length" class="toc-toggle" :aria-expanded="openKeys.has(row.node.key)" :aria-label="openKeys.has(row.node.key) ? '收起子章节' : '展开子章节'" @click="toggle(row.node, $event)">
            <AppIcon :name="openKeys.has(row.node.key) ? 'chevronDown' : 'chevronRight'" :size="14" />
          </button>
          <span v-else class="toc-toggle-space" aria-hidden="true" />
          <a :href="`#${row.node.slug}`">{{ row.node.title }}</a>
        </div>
      </TransitionGroup>
      <p v-if="!rows.length" class="toc-empty">这篇笔记没有分节标题</p>
    </nav>
    <div class="toc-pill-label" aria-hidden="true">
      <AppIcon name="library" :size="15" /><span>目录</span>
    </div>
  </div>
</template>
