<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import type { HighlightColor, StickyNote } from '@/lib/annotations'

const props = defineProps<{ item: StickyNote; anchor?: { x: number; y: number }; index?: number }>()
const emit = defineEmits<{
  update: [body: string]
  recolor: [color: HighlightColor]
  remove: []
  hover: [active: boolean]
}>()

const WIDTH = 224
const collapsed = ref(false)
const hovered = ref(false)
const dragging = ref(false)
const x = ref(0)
const y = ref(0)
const height = ref<number | null>(null)
const expandedHeight = ref(0)
const cardRef = ref<HTMLElement>()
const textareaRef = ref<HTMLTextAreaElement>()

const cardWidth = ref(WIDTH)

// 便条放到正文栏左右留白里，垂直对齐选中行；连线保持水平。
function place() {
  const anchor = props.anchor
  const layer = cardRef.value?.offsetParent as HTMLElement | null
  const layerRect = layer?.getBoundingClientRect()
  const mainWidth = layerRect?.width || 760
  const pageLeft = layerRect?.left || 0
  if (!anchor) {
    x.value = mainWidth + 12
    y.value = 120
    return
  }
  const rightMargin = Math.max(0, window.innerWidth - (pageLeft + mainWidth))
  const leftMargin = Math.max(0, pageLeft)
  const gap = 12
  const available = Math.max(160, (rightMargin >= leftMargin ? rightMargin : leftMargin) - gap * 2)
  cardWidth.value = Math.min(WIDTH, available)
  const useRight = rightMargin >= leftMargin
  // 胶囊和卡片保持在正文栏同一侧的留白里，只做宽高形变，不换边、不跑到屏幕边缘。
  if (collapsed.value) {
    x.value = useRight ? mainWidth + 8 : -36 - 8
    y.value = anchor.y - 9
  } else {
    x.value = useRight ? mainWidth + gap : -cardWidth.value - gap
    y.value = anchor.y - 18 + ((props.index || 0) % 4) * 14
  }
}

const positionStyle = computed(() => ({
  left: `${x.value}px`,
  top: `${y.value}px`,
  width: `${collapsed.value ? 36 : cardWidth.value}px`,
  ...(height.value !== null ? { height: `${height.value}px` } : {}),
}))

function onInput(event: Event) {
  emit('update', (event.target as HTMLTextAreaElement).value)
}

function measure() {
  if (!collapsed.value && cardRef.value) expandedHeight.value = cardRef.value.offsetHeight
}

async function minimize() {
  const element = cardRef.value
  if (!element) return
  expandedHeight.value = element.offsetHeight
  height.value = expandedHeight.value
  await nextTick()
  collapsed.value = true
  height.value = 18
}

async function expand() {
  collapsed.value = false
  height.value = 18
  await nextTick()
  height.value = expandedHeight.value || 180
}

function toggle() {
  if (collapsed.value) expand()
  else minimize()
}

function onTransitionEnd(event: TransitionEvent) {
  if (event.target !== cardRef.value || event.propertyName !== 'height') return
  if (!collapsed.value) height.value = null
}

let drag: { startX: number; startY: number; originX: number; originY: number; moved: boolean } | null = null
let suppressClick = false

function startDrag(event: PointerEvent) {
  if (event.button !== 0) return
  if (!collapsed.value && (event.target as HTMLElement).closest('button')) return
  if (collapsed.value) return
  event.preventDefault()
  dragging.value = true
  drag = { startX: event.clientX, startY: event.clientY, originX: x.value, originY: y.value, moved: false }
  const element = cardRef.value
  if (element) element.style.transition = 'none'
  window.addEventListener('pointermove', onDrag)
  window.addEventListener('pointerup', stopDrag, { once: true })
}

function onDrag(event: PointerEvent) {
  if (!drag) return
  const dx = event.clientX - drag.startX
  const dy = event.clientY - drag.startY
  if (Math.abs(dx) + Math.abs(dy) > 4) drag.moved = true
  x.value = drag.originX + dx
  y.value = drag.originY + dy
  if (cardRef.value) {
    cardRef.value.style.left = `${x.value}px`
    cardRef.value.style.top = `${y.value}px`
  }
}

// 松手回位：重新贴回划线位置，不做自由摆放。
function stopDrag() {
  window.removeEventListener('pointermove', onDrag)
  const moved = drag?.moved || false
  drag = null
  dragging.value = false
  if (cardRef.value) cardRef.value.style.transition = ''
  place()
  if (moved) {
    suppressClick = true
    setTimeout(() => { suppressClick = false }, 0)
  }
}

function onClick() {
  if (collapsed.value && !suppressClick) expand()
}

watch(() => props.anchor, () => place(), { deep: true })

onMounted(() => {
  place()
  nextTick(measure)
  window.addEventListener('resize', place)
  window.addEventListener('scroll', place, { passive: true })
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', place)
  window.removeEventListener('scroll', place)
  window.removeEventListener('pointermove', onDrag)
})
</script>

<template>
  <div
    ref="cardRef"
    class="sticky-float"
    :class="[`sticky-${item.color}`, { collapsed }]"
    :style="positionStyle"
    @mouseenter="hovered = true; emit('hover', true)"
    @mouseleave="hovered = false; emit('hover', false)"
    role="note"
    :aria-label="`便条 ${item.body.slice(0, 20)}`"
    @transitionend="onTransitionEnd"
    @click="onClick"
  >
    <header class="sticky-float-head" @pointerdown="startDrag">
      <span class="sticky-dot" aria-hidden="true" />
      <strong v-if="!collapsed">便条</strong>
      <button v-if="!collapsed" class="icon-button" aria-label="最小化便条" @click.stop="minimize">
        <AppIcon name="minimize" :size="13" />
      </button>
      <button v-if="!collapsed" class="icon-button" aria-label="删除便条" @click.stop="emit('remove')">
        <AppIcon name="close" :size="13" />
      </button>
    </header>
    <div class="sticky-float-body">
      <textarea ref="textareaRef" :value="item.body" rows="4" placeholder="写点什么…" :disabled="collapsed" @input="onInput" />
      <footer>
        <div class="sticky-colors">
          <button v-for="color in ['yellow', 'green', 'blue', 'pink']" :key="color" :class="['color-dot', color, { active: item.color === color }]" :aria-label="`便条颜色 ${color}`" @click.stop="emit('recolor', color)" />
        </div>
      </footer>
    </div>
  </div>
</template>
