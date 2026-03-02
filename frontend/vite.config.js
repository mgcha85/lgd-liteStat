import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://backend:8080',
        changeOrigin: true
      },
      '/canvas': {
        target: 'http://canvas-analysis:8000',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/canvas/, '')
      },
      '/pattern': {
        target: 'http://map-pattern:8003',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/pattern/, '')
      }
    }
  }
})
