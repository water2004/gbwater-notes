<script setup lang="ts">
import { useHead } from '@unhead/vue'
import AppIcon from '@/components/AppIcon.vue'
useHead({ title: '页面未找到 · 碧山秋云' })
</script>

<template>
  <div class="not-found page-shell"><span>404</span><p class="eyebrow">PAGE NOT FOUND</p><h1>这一页不在笔记里</h1><p>链接可能已经调整，回到目录继续寻找需要的内容。</p><div><RouterLink to="/library" class="button button-primary">打开笔记库<AppIcon name="arrowRight" /></RouterLink><RouterLink to="/" class="button button-quiet">返回首页</RouterLink></div></div>
</template>
