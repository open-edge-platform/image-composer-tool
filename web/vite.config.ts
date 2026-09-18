import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // Listen on all interfaces so a remote browser (e.g. over SSH) can reach the
    // dev server at the host's IP without a tunnel. Harmless for local use.
    host: true,
    // Pin the dev-server port so the SSH port-forward is deterministic; fail
    // fast instead of silently hopping to the next free port.
    port: 5174,
    strictPort: true,
    // Proxy API calls to the Go backend during development.
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    // Emit into dist/ for embedding into the Go binary via embed.FS.
    outDir: 'dist',
  },
})
