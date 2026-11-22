<script setup lang="ts">
import { useHead } from '@unhead/vue'
import MediaLibrary from '@/components/MediaLibrary.vue'

useHead({ title: '图床 · 碧山秋云' })
</script>

<template>
  <div>
    <header class="admin-page-header"><div><h1>图床</h1><p>管理笔记图片。构建后，上传的图片会随静态站点发布。</p></div></header>
    <MediaLibrary />
  </div>
</template>
