import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// 两个独立入口：客户端 index.html、控制台 admin.html。
// 构建到 web/dist/，由 Go 侧 web/embed.go 以 //go:embed dist 打进二进制。
// 开发时 `npm run dev`（5173）通过 proxy 把 /api /cgi-bin /admin/* /ws 转发到 Go（7788）。
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        index: 'index.html',
        admin: 'admin.html',
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:7788',
      '/cgi-bin': 'http://127.0.0.1:7788',
      '/admin/': 'http://127.0.0.1:7788',
      '/ws': { target: 'ws://127.0.0.1:7788', ws: true },
    },
  },
});
