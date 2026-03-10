// frontend/vite.config.js
import { defineConfig } from 'vite'

export default defineConfig({
  server: {
    proxy: {
      '/api': {
        target: 'http://0.0.0.0:80',
        changeOrigin: true,
        secure: false,
      }
    }
  }
})