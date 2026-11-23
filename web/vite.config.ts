import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import { deploymentPaths } from './scripts/deployment.ts'

const repo = fileURLToPath(new URL('..', import.meta.url))
const web = fileURLToPath(new URL('.', import.meta.url))
const defaultCategories = fileURLToPath(new URL('../internal/app/default_categories.json', import.meta.url))
export default defineConfig(({ command, mode }) => {
  const env = loadEnv(mode, repo, ['GBWATER_BASE_PATH', 'GBWATER_API_BASE_PATH'])
  const { site, api } = deploymentPaths(env)
  const compiling = command === 'build'
  return {
    base: compiling ? './' : site,
    publicDir: compiling ? false : 'public',
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
        ignored: ['**/compiled/**', '**/dist/**', '**/src/generated/**'],
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
      outDir: 'compiled',
      emptyOutDir: true,
      chunkSizeWarningLimit: 900,
    },
  }
})
