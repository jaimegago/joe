import path from 'path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The dev server proxies the API path prefix to joe-core so the browser
// talks to a single origin (the Vite dev server) and never makes a
// cross-origin request — no CORS configuration is needed on the backend.
// Target defaults to joe-core's local address and is overridable via env
// for non-default backend locations.
const apiProxyTarget = process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:7777'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: apiProxyTarget,
        changeOrigin: true,
      },
    },
  },
})
