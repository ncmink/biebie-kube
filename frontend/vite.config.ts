import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import wails from '@wailsio/runtime/plugins/vite'

export default defineConfig({
  server: {
    host: '127.0.0.1',
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  // The Wails plugin keeps ./bindings in step with the Go services during
  // `wails3 dev`, so a new service method is callable without a manual step.
  plugins: [vue(), tailwindcss(), wails('./bindings')],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@bindings': fileURLToPath(new URL('./bindings', import.meta.url)),
    },
  },
  build: {
    // Monaco and xterm are loaded with the tab that needs them, so the
    // application shell stays small even though the editor chunk is large.
    // Assets are served from the binary, so its size costs no download.
    chunkSizeWarningLimit: 2600,
  },
})
