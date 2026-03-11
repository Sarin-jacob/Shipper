// frontend/vite.config.js
import { defineConfig } from 'vite'
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [
    tailwindcss(),
  ],
  server: {
    proxy: {
      '/api': {
        target: 'https://build.jell0.online',
        changeOrigin: true,
        secure: false,
      }
    }
  }
})