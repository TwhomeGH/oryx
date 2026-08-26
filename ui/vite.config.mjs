//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The base URL and locale are injected per build by Makefile:
//   PUBLIC_URL=/mgmt REACT_APP_LOCALE=zh BUILD_PATH=build/zh ...
// Keep the CRA-compatible variables, so that no source code changes required.
export default defineConfig(({ mode }) => {
  const publicUrl = (process.env.PUBLIC_URL || '/mgmt/').replace(/\/$/, '');

  return {
    plugins: [
      react(),
      {
        // Replace CRA style placeholders in index.html, such as %PUBLIC_URL%
        // and %REACT_APP_LOCALE%, which are used by source code as globals.
        // Must run before vite:build-html, whose decodeURI chokes on the raw
        // percent placeholders.
        name: 'oryx-html-env',
        enforce: 'pre',
        transform(code, id) {
          if (!id.endsWith('index.html')) {
            return null;
          }
          const replaced = code
            .replaceAll('%PUBLIC_URL%', publicUrl)
            .replaceAll('%REACT_APP_LOCALE%', process.env.REACT_APP_LOCALE || '');
          console.log(`[oryx-html-env] placeholders replaced, locale=${process.env.REACT_APP_LOCALE}`);
          return { code: replaced, map: null };
        },
      },
    ],
    // Allow REACT_APP_* variables, compatible with previous CRA usage.
    envPrefix: 'REACT_APP_',
    // All sources are .js files containing JSX (CRA legacy), tell esbuild to
    // parse them as JSX with the automatic React runtime.
    esbuild: {
      loader: 'jsx',
      include: [/src\/.*\.js$/],
      exclude: [],
      jsx: 'automatic',
    },
    optimizeDeps: {
      esbuildOptions: {
        loader: { '.js': 'jsx' },
        jsx: 'automatic',
      },
    },
    // Serve or publish under /mgmt/, the same as PUBLIC_URL of CRA.
    base: `${publicUrl}/`,
    define: {
      'process.env.REACT_APP_LOCALE': JSON.stringify(process.env.REACT_APP_LOCALE || ''),
      'process.env.PUBLIC_URL': JSON.stringify(publicUrl),
    },
    build: {
      outDir: process.env.BUILD_PATH || 'build',
      emptyOutDir: true,
    },
    server: {
      port: 3000,
      // Proxy for development, the same as previous src/setupProxy.js.
      proxy: [
        { context: ['/console'], target: 'http://127.0.0.1:2022', changeOrigin: true },
        { context: ['/players'], target: 'http://127.0.0.1:2022', changeOrigin: true },
        { context: ['/terraform'], target: 'http://127.0.0.1:2022', changeOrigin: true },
        { context: ['/tools'], target: 'http://127.0.0.1:2022', changeOrigin: true },
        { context: ['/api'], target: 'http://127.0.0.1:2022', changeOrigin: true },
        { context: ['/rtc'], target: 'http://127.0.0.1:2022', changeOrigin: true },
      ],
    },
    test: {
      environment: 'jsdom',
      globals: true,
      setupFiles: './src/setupTests.js',
    },
  };
});
