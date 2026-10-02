import { createStore } from './store.js';
import { ES } from './lang-es.js';

// UI strings are written in English in the code; other languages map English → translation.
// Missing entries fall back to English. Placeholders: {name}.
const DICTS = { en: null, es: ES };
export const LANGS = [['en', 'English'], ['es', 'Español']];

function initialLang() {
  try {
    const saved = localStorage.getItem('audioviz.lang');
    if (saved && saved in DICTS) return saved;
  } catch (e) { /* storage unavailable */ }
  return 'en';
}

export const langStore = createStore(initialLang());

export function setLang(l) {
  try { localStorage.setItem('audioviz.lang', l); } catch (e) { /* ignore */ }
  document.documentElement.lang = l;
  langStore.set(l);
}

export function t(s, vars) {
  const d = DICTS[langStore.get()];
  let out = (d && d[s]) || s;
  if (vars) for (const k in vars) out = out.split('{' + k + '}').join(vars[k]);
  return out;
}
