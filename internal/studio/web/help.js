import { html, useEffect } from './vendor/preact-htm.js';
import { createStore, useStore } from './store.js';
import { Icon } from './icons.js';
import { openMenu } from './menu.js';
import { t } from './i18n.js';

// Help of the studio's elements: no explanations on screen, they are on each element's
// right-click menu (Help), shown in a small dialog. { title, text } (both already translated)
export const helpStore = createStore(null);
export const showHelp = (title, text) => helpStore.set({ title, text });

// The Help entry of a right-click menu (null without a text)
export const helpItem = (title, text) => (text ? { label: t('Help'), icon: 'circle-help', onClick: () => showHelp(title, text) } : null);

// A help text translated paragraph by paragraph (one per line)
export const tHelp = (text) => (text ? String(text).split('\n').map((p) => t(p)).join('\n') : text);

// A right-click handler with just its Help (text not yet translated)
export const helpMenu = (title, text) => (e) => openMenu(e, [helpItem(title, tHelp(text))]);

// Mounted once by the app
export function HelpDialog() {
  const h = useStore(helpStore);
  useEffect(() => {
    if (!h) return;
    const k = (e) => { if (e.key === 'Escape') { e.stopPropagation(); helpStore.set(null); } };
    addEventListener('keydown', k, true);
    return () => removeEventListener('keydown', k, true);
  }, [h]);
  if (!h) return null;
  return html`<div class="modal-bg help-bg" onMouseDown=${(e) => e.target === e.currentTarget && helpStore.set(null)}>
    <div class="modal help-dlg">
      <div class="modal-h"><${Icon} name="circle-help" size=${16} cls="muted" />${h.title}<span class="spacer"></span>
        <button class="btn icon ghost" title=${t('Close (Esc)')} onClick=${() => helpStore.set(null)}><${Icon} name="x" size=${16} /></button></div>
      <div class="mb">${String(h.text).split('\n').map((p) => html`<p>${p}</p>`)}</div>
    </div>
  </div>`;
}
