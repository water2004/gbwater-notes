<script setup lang="ts">
import { onBeforeUnmount, onMounted, onUpdated, ref } from 'vue'

const props = defineProps<{ expanded: string }>()
const grid = ref<HTMLElement>()
let observer: ResizeObserver | undefined
let observed = new Set<HTMLElement>()
let lastExpanded = ''
let lastWidth = 0
let frame = 0

function layout() {
  const root = grid.value
  if (!root) return
  const cards = Array.from(root.children) as HTMLElement[]
  const width = root.clientWidth
  if (!width || !cards.length) return
  const style = getComputedStyle(root)
  const gap = parseFloat(style.columnGap)
  const limit = Number(style.getPropertyValue('--columns'))
  const runs = props.expanded ? [cards.slice(0, 1), cards.slice(1)] : [cards]
  const animate = lastWidth === width && lastExpanded !== props.expanded && !matchMedia('(prefers-reduced-motion: reduce)').matches
  const previous = animate ? cards.map(card => card.getBoundingClientRect()) : []
  lastWidth = width
  lastExpanded = props.expanded

  // Set every width before measuring. Wrapped titles, web fonts and the open
  // directory can all change a card's natural height at its new width.
  root.dataset.packed = ''
  const bands = runs.filter(run => run.length).map(run => {
    const columns = Math.min(limit, run.length)
    const step = (width + gap) / columns
    // Give a substantial preview an extra lane when the last row would be
    // sparse. Only span empty lanes at the top, so no gaps can be sealed below.
    let spare = (columns - run.length % columns) % columns
    let emptyLanes = columns
    const tiles = run.map(card => {
      const wide = columns > 2 && spare > 0 && emptyLanes > 1 && card.hasAttribute('data-wide-preview')
      const span = wide ? 2 : 1
      if (wide) spare--
      emptyLanes -= span
      card.style.width = span * step - gap + 'px'
      return { card, span }
    })
    return { tiles, columns, step }
  })
  const heights = new Map(cards.map(card => [card, card.getBoundingClientRect().height]))
  let top = 0
  for (const { tiles, columns, step } of bands) {
    const bottoms = Array<number>(columns).fill(top)
    for (const { card, span } of tiles) {
      const column = bottoms.indexOf(Math.min(...bottoms))
      card.style.left = column * step + 'px'
      card.style.top = bottoms[column] + 'px'
      bottoms.fill(bottoms[column] + heights.get(card)! + gap, column, column + span)
    }
    top = Math.max(...bottoms)
  }
  root.style.height = top - gap + 'px'
  if (animate) {
    const positions = cards.map(card => ({ x: card.offsetLeft, y: card.offsetTop }))
    const origin = root.getBoundingClientRect()
    cards.forEach((card, index) => {
      const dx = previous[index].left - origin.left - positions[index].x
      const dy = previous[index].top - origin.top - positions[index].y
      card.getAnimations().filter(animation => animation.id === 'shelf-reflow').forEach(animation => animation.cancel())
      if (Math.abs(dx) + Math.abs(dy) > 1) {
        card.animate([{ transform: `translate(${dx}px,${dy}px)` }, { transform: 'none' }], { id: 'shelf-reflow', duration: 230, easing: 'cubic-bezier(.22,.7,.3,1)' })
      }
    })
  }
}

function syncCards() {
  const cards = new Set(Array.from(grid.value?.children || []) as HTMLElement[])
  for (const card of observed) if (!cards.has(card)) observer?.unobserve(card)
  for (const card of cards) if (!observed.has(card)) observer?.observe(card)
  observed = cards
  layout()
}

onMounted(() => {
  if (!grid.value || typeof ResizeObserver === 'undefined') return
  observer = new ResizeObserver(() => {
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(layout)
  })
  observer.observe(grid.value)
  syncCards()
})
onUpdated(() => { if (observer) syncCards() })
onBeforeUnmount(() => {
  observer?.disconnect()
  if (frame) cancelAnimationFrame(frame)
})
</script>

<template>
  <div ref="grid" class="shelf-grid"><slot /></div>
</template>
