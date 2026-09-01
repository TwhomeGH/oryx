//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//

import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The base URL is injected per build by Makefile:
//   PUBLIC_URL=/mgmt BUILD_PATH=build ...
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
        // percent placeholders. The transform hook (enforce: pre) covers the
        // production build; transformIndexHtml covers the dev server, where
        // the browser requests %PUBLIC_URL%/manifest.json directly.
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
        transformIndexHtml(html) {
          return html
            .replaceAll('%PUBLIC_URL%', publicUrl)
            .replaceAll('%REACT_APP_LOCALE%', process.env.REACT_APP_LOCALE || '');
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
      // The platform default is 127.0.0.1:2022 (native); override with
      // SRS_PLATFORM to reach a Docker-mapped port, e.g.:
      //   SRS_PLATFORM=http://127.0.0.1:882 npm start
      proxy: {
        '/console': {target: process.env.SRS_PLATFORM || 'http://127.0.0.1:2022', changeOrigin: true},
        '/players': {target: process.env.SRS_PLATFORM || 'http://127.0.0.1:2022', changeOrigin: true},
        '/terraform': {target: process.env.SRS_PLATFORM || 'http://127.0.0.1:2022', changeOrigin: true},
        '/tools': {target: process.env.SRS_PLATFORM || 'http://127.0.0.1:2022', changeOrigin: true},
        '/api': {target: process.env.SRS_PLATFORM || 'http://127.0.0.1:2022', changeOrigin: true},
        '/rtc': {target: process.env.SRS_PLATFORM || 'http://127.0.0.1:2022', changeOrigin: true},
      },
    },
    test: {
      environment: 'jsdom',
      globals: true,
      setupFiles: './src/setupTests.js',
    },
  };
});
