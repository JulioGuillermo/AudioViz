import { useState, useEffect } from './vendor/preact-htm.js';

// Minimal observable value. Used for the playback time so that only the components that
// really need it (preview, clock, playhead) re-render 60 times per second.
export function createStore(initial) {
  let value = initial;
  const subs = new Set();
  return {
    get: () => value,
    set: (v) => {
      if (v === value) return;
      value = v;
      subs.forEach((f) => f(v));
    },
    sub: (f) => { subs.add(f); return () => subs.delete(f); },
  };
}

export function useStore(store) {
  const [v, setV] = useState(store.get());
  useEffect(() => { setV(store.get()); return store.sub(setV); }, [store]);
  return v;
}

// Same as useStore but throttled to at most one update every `ms` while `active` is true
export function useThrottledStore(store, ms, active) {
  const [v, setV] = useState(store.get());
  useEffect(() => {
    let last = 0, timer = null;
    const push = (x) => {
      if (!active) { setV(x); return; }
      const now = performance.now();
      clearTimeout(timer);
      if (now - last >= ms) { last = now; setV(x); } else timer = setTimeout(() => { last = performance.now(); setV(store.get()); }, ms - (now - last));
    };
    setV(store.get());
    const off = store.sub(push);
    return () => { off(); clearTimeout(timer); };
  }, [store, ms, active]);
  return v;
}
