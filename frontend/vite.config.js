import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发模式下 /api 代理到本机后端；生产由 nginx 容器代理。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true }
    }
  }
})
