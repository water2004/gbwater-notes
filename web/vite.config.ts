import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { apiPlaceholder, deploymentPaths, dynamicAssets, sitePlaceholder } from './scripts/deployment.ts'

const repo = fileURLToPath(new URL('..', import.meta.url))
const web = fileURLToPath(new URL('.', import.meta.url))
const defaultCategories = fileURLToPath(new URL('../internal/app/default_categories.json', import.meta.url))
export default defineConfig(({ mode }) => {
  const template = process.env.GBWATER_DYNAMIC_FRONTEND === 'template'
  const env = loadEnv(mode, repo, ['GBWATER_BASE_PATH', 'GBWATER_API_BASE_PATH'])
  const { site: base, api } = template
    ? { site: sitePlaceholder, api: apiPlaceholder }
    : deploymentPaths(env)
  return {
    base,
    define: { 'import.meta.env.VITE_API_BASE_PATH': JSON.stringify(api) },
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
        [api]: {
          target: 'http://127.0.0.1:8080',
          changeOrigin: false,
          xfwd: true,
          rewrite: path => `/api/${path.slice(api.length)}`,
        },
      },
    },
    build: {
      outDir: 'dist',
      assetsDir: template ? dynamicAssets : 'assets',
      emptyOutDir: true,
      chunkSizeWarningLimit: 900,
    },
    ssgOptions: {
      dirStyle: 'nested',
      beastiesOptions: false,
    },
  }
})
