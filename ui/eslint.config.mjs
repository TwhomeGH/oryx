//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
// Flat config for eslint 9. Replaces the deprecated eslint-config-react-app.
import js from '@eslint/js';
import react from 'eslint-plugin-react';
import reactHooks from 'eslint-plugin-react-hooks';
import globals from 'globals';

export default [
  {
    ignores: ['build/**', 'node_modules/**'],
  },
  {
    // Legacy code carries many stale eslint-disable comments.
    linterOptions: {reportUnusedDisableDirectives: 'off'},
  },
  js.configs.recommended,
  {
    files: ['src/**/*.js'],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      parserOptions: {
        ecmaFeatures: {jsx: true},
      },
      globals: {
        ...globals.browser,
        // Vite replaces process.env.* at build time.
        process: 'readonly',
        // CRA/jest compatibility for tests.
        describe: 'readonly',
        it: 'readonly',
        test: 'readonly',
        expect: 'readonly',
        jest: 'readonly',
      },
    },
    plugins: {
      react,
      'react-hooks': reactHooks,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      'react-hooks/exhaustive-deps': 'off',
      // Mark JSX tag names as variable usages, otherwise every component
      // import is falsely reported as unused.
      'react/jsx-uses-vars': 'error',
      'react/jsx-uses-react': 'error',
      'no-unused-vars': ['error', {args: 'none', varsIgnorePattern: '^_'}],
      // Legacy style allowances, matching the previous react-app preset.
      'no-empty': ['error', {allowEmptyCatch: true}],
      'no-constant-binary-expression': 'off',
      'no-constant-condition': 'off',
    },
  },
];
