import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:18780',
        changeOrigin: true,
        ws: true,
      },
      '/healthz': 'http://127.0.0.1:18780',
      '/readyz': 'http://127.0.0.1:18780',
      '/metrics': 'http://127.0.0.1:18780',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    exclude: ['e2e/**', 'node_modules/**', 'dist/**'],
    css: true,
  },
})
