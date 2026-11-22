<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'

export interface MenuItem {
  label: string
  action: () => void
  danger?: boolean
}

defineProps<{ x: number; y: number; items: MenuItem[] }>()
const emit = defineEmits<{ close: [] }>()

function run(item: MenuItem) {
  emit('close')
  item.action()
}

function onKey(event: KeyboardEvent) {
  if (event.key === 'Escape') emit('close')
}

function onPointerDown(event: PointerEvent) {
  const target = event.target as HTMLElement
  if (!target.closest('.context-menu')) emit('close')
}

onMounted(() => {
  window.addEventListener('keydown', onKey)
  window.addEventListener('pointerdown', onPointerDown, true)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('pointerdown', onPointerDown, true)
})
</script>

<template>
  <Teleport to="body">
    <div class="context-menu" :style="{ left: `${x}px`, top: `${y}px` }" role="menu" @pointerdown.stop @contextmenu.prevent>
      <button v-for="item in items" :key="item.label" :class="{ danger: item.danger }" role="menuitem" @click="run(item)">{{ item.label }}</button>
    </div>
  </Teleport>
</template>
