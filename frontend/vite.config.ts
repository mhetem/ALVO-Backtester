import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { defineConfig, type Plugin } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

const apiTarget = process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080';

function keepDist(): Plugin {
  let outDir = 'dist';
  return {
    name: 'alvo-keep-dist',
    apply: 'build',
    configResolved(config) {
      outDir = config.build.outDir;
    },
    closeBundle() {
      writeFileSync(resolve(outDir, '.gitkeep'), '');
    },
  };
}

export default defineConfig({
  plugins: [svelte(), keepDist()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
      },
    },
  },
});
