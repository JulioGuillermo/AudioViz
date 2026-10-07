import { html, useEffect, useLayoutEffect, useRef } from './vendor/preact-htm.js';
import { createStore, useStore } from './store.js';
import { Icon } from './icons.js';

// Context menus of the studio. The browser's own menu is turned off everywhere except in text
// fields (copy / paste); right-click on things opens these menus instead.
//
//   openMenu(e, [{ label, icon, onClick, danger, disabled, checked }, '-', { header: 'Title' }, ...])
//
// icon: an Icon name, or any html node (e.g. a curve drawn as SVG). '-' is a separator.

const menuStore = createStore(null);

export function openMenu(e, items) {
  e.preventDefault();
  e.stopPropagation();
  menuStore.set({ x: e.clientX, y: e.clientY, items: items.filter(Boolean) });
}
export const closeMenu = () => menuStore.set(null);

const editable = (el) => el && (el.isContentEditable || /^(INPUT|TEXTAREA)$/.test(el.tagName) &&
  !/^(range|checkbox|radio|color|button|file)$/i.test(el.type || ''));

// Mounted once by the app
export function ContextMenu() {
  const menu = useStore(menuStore);
  const ref = useRef();
  useEffect(() => {
    const noNative = (e) => { if (!editable(e.target)) e.preventDefault(); };
    addEventListener('contextmenu', noNative);
    return () => removeEventListener('contextmenu', noNative);
  }, []);
  useEffect(() => {
    if (!menu) return;
    const close = () => closeMenu();
    const key = (e) => { if (e.key === 'Escape') closeMenu(); };
    addEventListener('mousedown', close);
    addEventListener('wheel', close, { passive: true });
    addEventListener('resize', close);
    addEventListener('blur', close);
    addEventListener('keydown', key);
    return () => {
      removeEventListener('mousedown', close); removeEventListener('wheel', close);
      removeEventListener('resize', close); removeEventListener('blur', close); removeEventListener('keydown', key);
    };
  }, [menu]);
  // Never outside the window: opens upwards / to the left when there is no room, and is pushed
  // inside the edges if it still does not fit (taller than the window: it scrolls)
  useLayoutEffect(() => {
    const el = ref.current;
    if (!menu || !el) return;
    const m = 6, w = el.offsetWidth, h = el.offsetHeight;
    let x = menu.x, y = menu.y;
    if (x + w > innerWidth - m) x = menu.x - w;
    if (y + h > innerHeight - m) y = menu.y - h;
    el.style.left = Math.max(m, Math.min(x, innerWidth - w - m)) + 'px';
    el.style.top = Math.max(m, Math.min(y, innerHeight - h - m)) + 'px';
    el.style.visibility = 'visible';
  }, [menu]);
  if (!menu) return null;
  return html`<div class="ctxmenu" ref=${ref} style=${{ left: menu.x + 'px', top: menu.y + 'px', visibility: 'hidden' }}
      onMouseDown=${(e) => e.stopPropagation()} onContextMenu=${(e) => e.preventDefault()}>
    ${menu.items.map((it, i) => {
      if (it === '-') return html`<div class="sep" key=${i}></div>`;
      if (it.header) return html`<div class="ctxh" key=${i}>${it.header}</div>`;
      return html`<button key=${i} class=${(it.danger ? 'danger' : '') + (it.checked ? ' on' : '')} disabled=${it.disabled}
          onClick=${() => { closeMenu(); it.onClick && it.onClick(); }}>
        <span class="mi">${typeof it.icon === 'string' ? html`<${Icon} name=${it.icon} size=${14} />` : it.icon || ''}</span>
        <span class="ml">${it.label}</span>${it.hint && html`<span class="mk">${it.hint}</span>`}</button>`;
    })}
  </div>`;
}
