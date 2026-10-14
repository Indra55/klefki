import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
export default defineConfig({build:{assetsInlineLimit:0},plugins:[react()],test:{include:['src/**/*.test.{ts,tsx}'],environment:'jsdom',setupFiles:['./src/test-setup.ts']}});
