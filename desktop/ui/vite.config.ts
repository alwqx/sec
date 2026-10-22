import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { readFileSync } from 'node:fs'

// 清理 dist 前读取入库占位文件，随本次构建重新输出，避免删除受版本控制的文件。
const placeholder = readFileSync(new URL('./dist/placeholder.txt', import.meta.url), 'utf8')

// dev 模式下把 /api 代理到 Go server（固定 127.0.0.1:3001）。
export default defineConfig({
  plugins: [
    react(),
    {
      name: 'preserve-dist-placeholder',
      generateBundle() {
        this.emitFile({ type: 'asset', fileName: 'placeholder.txt', source: placeholder })
      },
    },
  ],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:3001',
    },
  },
  build: {
    // 每次构建清空旧产物，避免旧 hash 资源被嵌入桌面安装包。
    emptyOutDir: true,
  },
})
