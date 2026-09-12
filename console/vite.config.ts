import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { resolve } from 'node:path';

// Built into internal/console/dist, which embed.FS picks up so that one binary
// includes the UI (R-253).
export default defineConfig({
  plugins: [react()],

  resolve: {
    alias: {
      // The design system's barrel. Components are plain .jsx with sibling
      // .d.ts files; TypeScript reads the declarations, Vite compiles the JSX.
      '@design': resolve(__dirname, 'src/design/index.js'),
      '@api': resolve(__dirname, 'src/api'),
    },
  },

  build: {
    outDir: resolve(__dirname, '../internal/console/dist'),
    emptyOutDir: true,

    // No source maps in the shipped binary: they would double its size and the
    // console is not debugged in production.
    sourcemap: false,
  },

  server: {
    // `npm run dev` proxies to a locally running Pando so the console can be
    // developed against a real API rather than fixtures.
    proxy: {
      '/api': { target: 'http://localhost:8099', changeOrigin: true },
      '/.well-known': { target: 'http://localhost:8099', changeOrigin: true },
    },
  },
});
