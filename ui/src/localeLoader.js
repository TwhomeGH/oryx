//
// Copyright (c) 2022-2026 Winlin
//
// SPDX-License-Identifier: MIT
//
// Locale loader: dynamically scans all resources/locale_*.json files and
// builds the i18next resources object plus the available language list.
//
// Each locale file has the shape:
//   {
//     "translation": { ... i18n keys ... },
//     "meta": { "code": "zh", "name": "简体中文" }
//   }
// The lang key for i18next is taken from "meta.code" (or the filename suffix
// if meta is missing), so adding a new language is just dropping a new
// locale_*.json file in the folder. The language switcher then picks it up
// automatically.

// import.meta.glob is resolved statically at build time by Vite; every
// locale_*.json in resources/ is bundled. The {eager: true} makes the import
// a plain object (no async loading needed for a handful of small files).
const localeModules = import.meta.glob("./resources/locale_*.json", {eager: true});

// The i18next resources: { lang: { translation: {...} } }.
const resources = {};
// The language list for the switcher: [{code, name}].
const locales = [];

for (const path in localeModules) {
  const mod = localeModules[path];
  // Find the file suffix: locale_<suffix>.json
  const m = path.match(/locale_([a-zA-Z-]+)\.json$/);
  if (!m) continue;

  const code = mod.meta?.code || m[1];
  const name = mod.meta?.name || code;

  resources[code] = {translation: mod.translation || {}};
  locales.push({code, name});
}

// Keep the switcher order stable: sort by code.
locales.sort((a, b) => a.code.localeCompare(b.code));

console.log(`Locale loader loaded ${locales.length} languages: ${JSON.stringify(locales.map(e => e.code))}`);

export {resources, locales};
