<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ title: string }>()
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement>()
onMounted(() => dialog.value?.showModal())
</script>

<template>
  <dialog ref="dialog" class="admin-dialog" :aria-label="props.title" @cancel.prevent="emit('close')" @click.self="emit('close')">
    <div class="admin-dialog-inner">
      <header class="admin-dialog-header">
        <h2>{{ title }}</h2>
        <button type="button" class="icon-button" :aria-label="`关闭${title}`" @click="emit('close')"><AppIcon name="close" /></button>
      </header>
      <slot />
    </div>
  </dialog>
</template>
