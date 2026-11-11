import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  // Backend that "/api/*" is forwarded to. Backend runs on 8080.
  const target = env.VITE_PROXY_TARGET || 'http://localhost:8080'
  const proxy = {
    '/api': { target, changeOrigin: true, rewrite: (path: string) => path.replace(/^\/api/, '') },
  }
  return { plugins: [react()], server: { proxy }, preview: { proxy } }
})
