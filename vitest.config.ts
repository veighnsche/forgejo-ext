import {defineConfig} from 'vitest/config';
import vuePlugin from '@vitejs/plugin-vue';
import {stringPlugin} from 'vite-string-plugin';

export default defineConfig({
  test: {
    include: ['web_src/**/*.test.{js,ts}'],
    setupFiles: ['web_src/js/vitest.setup.js'],
    environment: 'happy-dom',
    testTimeout: 20000,
    open: false,
    allowOnly: false,
    passWithNoTests: false,
    globals: true,
    watch: false,
    mockReset: true,
  },
  plugins: [
    stringPlugin(),
    vuePlugin(),
  ],
});
