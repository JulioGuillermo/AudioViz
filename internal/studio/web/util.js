// this studio's id (collaboration): the jobs it starts show who started them
let client = '';
export const setClient = (id) => { client = id || ''; };
export const clientId = () => client;

export async function api(method, url, body) {
  const opt = { method, headers: client ? { 'X-AudioViz-Client': client } : {} };
  if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  const r = await fetch(url, opt);
  const ct = r.headers.get('Content-Type') || '';
  const data = ct.includes('json') ? await r.json() : await r.text();
  if (!r.ok) {
    const e = new Error((data && data.error) || r.statusText);
    e.status = r.status;
    throw e;
  }
  return data;
}

export const clone = (o) => (o === undefined ? o : JSON.parse(JSON.stringify(o)));

export function fmtTime(t, cs = true) {
  if (!isFinite(t)) t = 0;
  const neg = t < 0;
  t = Math.abs(t);
  const m = Math.floor(t / 60);
  const s = t - m * 60;
  const ss = cs ? s.toFixed(2).padStart(5, '0') : String(Math.floor(s)).padStart(2, '0');
  return (neg ? '-' : '') + `${m}:${ss}`;
}

export function parseTime(str) {
  str = String(str).trim();
  if (str.includes(':')) {
    const [m, s] = str.split(':');
    return (parseFloat(m) || 0) * 60 + (parseFloat(s) || 0);
  }
  return parseFloat(str) || 0;
}

export function b64u8(s) {
  if (!s) return new Uint8Array(0);
  const bin = atob(s);
  const a = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) a[i] = bin.charCodeAt(i);
  return a;
}

export const basename = (p) => (p ? String(p).split(/[\\/]/).pop() : ''); // separators / and \ (Windows)

export function debounce(fn, ms) {
  let t;
  const d = (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
  d.flush = (...a) => { clearTimeout(t); fn(...a); };
  return d;
}

// Hex #rgb / #rrggbb / #rrggbbaa -> {rgb:'#rrggbb', a:0..255}
export function splitHex(h) {
  h = String(h || '').replace('#', '');
  if (h.length === 3) h = h.split('').map((c) => c + c).join('');
  const rgb = '#' + (h.slice(0, 6).padEnd(6, '0'));
  const a = h.length === 8 ? parseInt(h.slice(6), 16) : 255;
  return { rgb, a: isNaN(a) ? 255 : a };
}
export const joinHex = (rgb, a) => (a >= 255 ? rgb : rgb + Math.round(a).toString(16).padStart(2, '0'));

export function gradientCSS(stops, dir = '90deg') {
  if (!stops || !stops.length) return 'transparent';
  const s = [...stops].sort((a, b) => a.offset - b.offset).map((g) => `${g.color} ${(g.offset * 100).toFixed(1)}%`);
  return `linear-gradient(${dir}, ${s.join(', ')})`;
}
