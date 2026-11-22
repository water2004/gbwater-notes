import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// 部署路径由构建期环境变量决定，默认 /mypdf/，便于同一份代码打进镜像后挂在任意子目录。
function normalizeBase(value: string): string {
  let base = value.trim() || '/'
  if (!base.startsWith('/')) base = `/${base}`
  if (!base.endsWith('/')) base = `${base}/`
  return base
}
const repo = fileURLToPath(new URL('..', import.meta.url))
const web = fileURLToPath(new URL('.', import.meta.url))
const defaultCategories = fileURLToPath(new URL('../internal/app/default_categories.json', import.meta.url))
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, repo, 'GBWATER_BASE_PATH')
  const base = normalizeBase(env.GBWATER_BASE_PATH || '/mypdf/')
  return {
    base,
    plugins: [vue()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      fs: { allow: [web, defaultCategories] },
      watch: {
        // Vite 只消费正式产物。构建暂存目录和 Go 私有渲染输入无需触发整页刷新。
        ignored: ['**/.dist-build-*/**', '**/src/generated/notes/**', '**/src/generated/notes-html/**'],
      },
      // 保留浏览器原始 Host，后端按主机名校验同源请求。
      proxy: {
        '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false, xfwd: true },
      },
    },
    build: {
      outDir: 'dist',
      emptyOutDir: true,
      chunkSizeWarningLimit: 900,
    },
    ssgOptions: {
      dirStyle: 'nested',
      beastiesOptions: false,
    },
  }
})
