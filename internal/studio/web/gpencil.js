import { segment, splitSegment, simplify } from './masks.js';

// Grease Pencil tools on the shapes of a vector mask (or a motion path): the stroke / point eraser,
// the scissors (cut up to the intersections) and the fill (a new shape from the lines around a
// point). Shapes are { points, open } in their own space (0..1); map(u, v) → frame px and
// back(x, y) → [u, v] convert, so distances are measured on screen.

const SUB = 48; // samples per segment
const segs = (sh) => (sh.open ? sh.points.length - 1 : sh.points.length);
const bez = (P, t) => {
  const u = 1 - t;
  return [0, 1].map((k) => u * u * u * P[0][k] + 3 * u * u * t * P[1][k] + 3 * u * t * t * P[2][k] + t * t * t * P[3][k]);
};
const copy = (sh) => ({ ...sh, points: sh.points.map((p) => [...p]) });

// A shape as a polyline in frame px: [{ x, y, s }] with s = segment index + t
export function polyline(sh, map) {
  const out = [];
  const n = segs(sh);
  for (let i = 0; i < n; i++) {
    const P = segment(sh.points, i, !!sh.open);
    for (let k = i === 0 ? 0 : 1; k <= SUB; k++) {
      const q = bez(P, k / SUB), f = map(q[0], q[1]);
      out.push({ x: f.x, y: f.y, s: i + k / SUB });
    }
  }
  return out;
}

const near = (pl, pts, r) => pl.some((a) => pts.some((b) => Math.hypot(a.x - b.x, a.y - b.y) <= r));

// Stroke eraser: the shapes the brush (frame points, radius r px) touches are removed
export function eraseStrokes(shapes, brush, r, map) {
  return shapes.filter((sh) => sh.points.length < 2 || !near(polyline(sh, map), brush, r));
}

// Point eraser: the nodes under the brush are removed; a closed shape opens there, an open one
// splits; pieces with fewer than two nodes go
export function erasePoints(shapes, brush, r, map) {
  const out = [];
  for (const sh of shapes) {
    const pts = sh.points;
    const gone = pts.map((p) => { const f = map(p[0], p[1]); return brush.some((b) => Math.hypot(f.x - b.x, f.y - b.y) <= r); });
    if (!gone.some(Boolean)) { out.push(sh); continue; }
    let order = pts.map((_, i) => i);
    if (!sh.open) {
      // start right after a removed node, so the kept runs do not wrap
      const k = gone.indexOf(true);
      order = [...order.slice(k + 1), ...order.slice(0, k + 1)];
    }
    let run = [];
    const flush = () => { if (run.length >= 2) out.push({ ...sh, open: true, points: run.map((i) => [...pts[i]]) }); run = []; };
    for (const i of order) { if (gone[i]) flush(); else run.push(i); }
    flush();
  }
  return out;
}

// Where two segments cross (a1-a2 and b1-b2): the fraction along a, or -1
function cross(a1, a2, b1, b2) {
  const dx = a2.x - a1.x, dy = a2.y - a1.y, ex = b2.x - b1.x, ey = b2.y - b1.y;
  const den = dx * ey - dy * ex;
  if (Math.abs(den) < 1e-9) return -1;
  const t = ((b1.x - a1.x) * ey - (b1.y - a1.y) * ex) / den;
  const u = ((b1.x - a1.x) * dy - (b1.y - a1.y) * dx) / den;
  return t >= 0 && t <= 1 && u >= 0 && u <= 1 ? t : -1;
}

// Where shape si crosses the shapes (and itself): its parameters s (segment + t), sorted
function crossings(shapes, si, map) {
  const lines = shapes.map((sh) => (sh.points.length >= 2 ? polyline(sh, map) : []));
  const A = lines[si], out = [];
  for (let i = 0; i + 1 < A.length; i++) {
    for (let j = 0; j < lines.length; j++) {
      const B = lines[j];
      for (let k = 0; k + 1 < B.length; k++) {
        // the same line: not its own neighbors (they always touch)
        if (j === si && (Math.abs(i - k) <= 1 || (!shapes[si].open && Math.abs(i - k) >= A.length - 2))) continue;
        const t = cross(A[i], A[i + 1], B[k], B[k + 1]);
        if (t >= 0) out.push(A[i].s + (A[i + 1].s - A[i].s) * t);
      }
    }
  }
  out.sort((a, b) => a - b);
  // the same crossing found twice (at the joint of two pieces): once
  return out.filter((s, k) => k === 0 || s - out[k - 1] > 1e-4);
}

// Nodes at the parameters ss of a shape (its segments split there; the curve stays exactly the
// same). sh is changed. Returns the final index of the node at each parameter, in order.
function nodesAt(sh, ss) {
  const n = segs(sh);
  const order = ss.map((s, k) => ({ s: Math.max(0, Math.min(n, s)), k })).sort((x, y) => y.s - x.s);
  const idx = new Array(ss.length), inserted = []; // [k, index when inserted]
  const cut = {}; // per segment, the lowest t split so far (later t are relative to it)
  for (const { s, k } of order) {
    let i = Math.min(Math.floor(s), n - 1), t = s - i;
    if (t > 1 - 1e-4) { i = (i + 1) % sh.points.length; t = 0; }
    if (t < 1e-4) { idx[k] = i; continue; }
    if (cut[i] !== undefined) t /= cut[i];
    cut[i] = s - Math.floor(s);
    const j = splitSegment(sh, i, t);
    // the nodes inserted before (higher up) move one place on
    for (const e of inserted) if (e[1] >= j) e[1]++;
    for (let q = 0; q < idx.length; q++) if (idx[q] !== undefined && idx[q] >= j && !inserted.some((e) => e[0] === q)) idx[q]++;
    inserted.push([k, j]);
  }
  for (const [k, j] of inserted) idx[k] = j;
  return idx;
}

// The nearest line to a frame point: { si, s, d } (shape, parameter, distance px)
export function nearestLine(shapes, f, map) {
  let best = null;
  shapes.forEach((sh, si) => {
    if (sh.points.length < 2) return;
    for (const p of polyline(sh, map)) {
      const d = Math.hypot(p.x - f.x, p.y - f.y);
      if (!best || d < best.d) best = { si, s: p.s, d };
    }
  });
  return best;
}

// Scissors: the piece of the line at f (within tol px) between the crossings on either side is
// removed (the whole line if it crosses nothing). Returns the new shapes, or null if nothing is there.
export function cutAt(shapes, f, tol, map) {
  const hit = nearestLine(shapes, f, map);
  if (!hit || hit.d > tol) return null;
  const sh = shapes[hit.si], rest = shapes.filter((_, k) => k !== hit.si);
  const xs = crossings(shapes, hit.si, map);
  if (!xs.length) return rest;
  const n = segs(sh);
  let a = [...xs].reverse().find((s) => s < hit.s), b = xs.find((s) => s > hit.s);
  const c = copy(sh);
  if (sh.open) {
    const ps = [a, b].filter((x) => x !== undefined), ix = nodesAt(c, ps);
    const ia = a !== undefined ? ix[0] : undefined, ib = b !== undefined ? ix[ps.length - 1] : undefined;
    const pieces = [];
    if (ia !== undefined) pieces.push(c.points.slice(0, ia + 1));
    if (ib !== undefined) pieces.push(c.points.slice(ib));
    return [...rest, ...pieces.filter((p) => p.length >= 2).map((p) => ({ ...c, open: true, points: p.map((q) => [...q]) }))];
  }
  // closed: the crossing before and after the click, around the loop
  if (a === undefined) a = xs[xs.length - 1];
  if (b === undefined) b = xs[0];
  if (Math.abs(a - b) < 1e-4) {
    // one crossing: the loop just opens there
    const [i] = nodesAt(c, [a]);
    return [...rest, { ...c, open: true, points: [...c.points.slice(i), ...c.points.slice(0, i + 1)].map((p) => [...p]) }];
  }
  const [ia, ib] = nodesAt(c, [a, b]);
  // kept: from b forward, around, to a
  const pts = c.points;
  const keep = ib <= ia ? pts.slice(ib, ia + 1) : [...pts.slice(ib), ...pts.slice(0, ia + 1)];
  return [...rest, { ...c, open: true, points: keep.map((p) => [...p]) }];
}

// Fill: the area around frame point f closed by the lines (drawn 2.5 px wide at 1/2 scale) becomes
// a new closed shape. Returns its points in shape space, or null if the area is not closed.
export function fillAt(shapes, f, W, H, map, back) {
  const k = 0.5, w = Math.ceil(W * k), h = Math.ceil(H * k);
  const cv = document.createElement('canvas');
  cv.width = w; cv.height = h;
  const cx = cv.getContext('2d');
  cx.strokeStyle = '#fff';
  cx.lineWidth = 2.5;
  cx.lineJoin = 'round';
  for (const sh of shapes) {
    const pl = polyline(sh, map);
    if (pl.length < 2) continue;
    cx.beginPath();
    pl.forEach((p, i) => (i ? cx.lineTo(p.x * k, p.y * k) : cx.moveTo(p.x * k, p.y * k)));
    if (!sh.open) cx.closePath();
    cx.stroke();
  }
  const wall = cx.getImageData(0, 0, w, h).data;
  const sx = Math.floor(f.x * k), sy = Math.floor(f.y * k);
  if (sx < 0 || sy < 0 || sx >= w || sy >= h || wall[(sy * w + sx) * 4 + 3] > 100) return null;
  // flood fill (4 neighbours); reaching the border = not closed
  const m = new Uint8Array(w * h), stack = [sy * w + sx];
  m[sy * w + sx] = 1;
  while (stack.length) {
    const i = stack.pop(), x = i % w, y = (i - x) / w;
    if (x === 0 || y === 0 || x === w - 1 || y === h - 1) return null;
    for (const j of [i - 1, i + 1, i - w, i + w]) {
      if (!m[j] && wall[j * 4 + 3] <= 100) { m[j] = 1; stack.push(j); }
    }
  }
  // its outline (Moore neighbour tracing from the top-left filled pixel), half a line out
  let s0 = m.indexOf(1);
  const D = [[1, 0], [1, 1], [0, 1], [-1, 1], [-1, 0], [-1, -1], [0, -1], [1, -1]];
  const at = (x, y) => x >= 0 && y >= 0 && x < w && y < h && m[y * w + x] === 1;
  let x = s0 % w, y = (s0 - x) / w, dir = 6;
  const ring = [];
  for (let guard = 0; guard < w * h * 2; guard++) {
    ring.push({ x, y });
    let moved = false;
    for (let r = 0; r < 8; r++) {
      const d = (dir + 6 + r) % 8, nx = x + D[d][0], ny = y + D[d][1];
      if (at(nx, ny)) { x = nx; y = ny; dir = d; moved = true; break; }
    }
    if (!moved || (x === ring[0].x && y === ring[0].y && ring.length > 2)) break;
  }
  if (ring.length < 6) return null;
  const pts = simplify(ring.map((p) => ({ x: (p.x + 0.5) / k, y: (p.y + 0.5) / k })), 1.5 / k);
  if (pts.length < 4) return null;
  pts.pop(); // the last one is the first again
  // corner nodes: the outline exactly (smooth ones would bulge across the lines around)
  return pts.map((p) => [...back(p.x, p.y).map((v) => +v.toFixed(5)), 1]);
}

// Bucket fill on a painted mask (canvas of the mask): the connected area of similar alpha around
// (px, py) gets the given alpha (0..1). Returns false if the point is outside.
export function bucketFill(cv, px, py, alpha) {
  const w = cv.width, h = cv.height, x0 = Math.floor(px), y0 = Math.floor(py);
  if (x0 < 0 || y0 < 0 || x0 >= w || y0 >= h) return false;
  const cx = cv.getContext('2d'), img = cx.getImageData(0, 0, w, h), d = img.data;
  const a0 = d[(y0 * w + x0) * 4 + 3], tol = 40, v = Math.round(alpha * 255);
  const m = new Uint8Array(w * h), stack = [y0 * w + x0];
  m[y0 * w + x0] = 1;
  while (stack.length) {
    const i = stack.pop(), x = i % w;
    d[i * 4] = d[i * 4 + 1] = d[i * 4 + 2] = 255;
    d[i * 4 + 3] = Math.max(d[i * 4 + 3], v);
    const nb = [x > 0 ? i - 1 : -1, x < w - 1 ? i + 1 : -1, i - w, i + w];
    for (const j of nb) {
      if (j >= 0 && j < w * h && !m[j] && Math.abs(d[j * 4 + 3] - a0) <= tol) { m[j] = 1; stack.push(j); }
    }
  }
  cx.putImageData(img, 0, 0);
  return true;
}

// The piece of a shape between parameters s0 < s1 (an open line; the curve stays the same). For a
// closed shape s1 may be past its end (it wraps).
function piece(sh, s0, s1) {
  const c = copy(sh), n = segs(sh);
  if (!sh.open && s1 > n) {
    const [i0, i1] = nodesAt(c, [s0, s1 - n]);
    return { ...c, open: true, points: [...c.points.slice(i0), ...c.points.slice(0, i1 + 1)].map((p) => [...p]) };
  }
  const [i0, i1] = nodesAt(c, [s0, s1]);
  return { ...c, open: true, points: c.points.slice(i0, i1 + 1).map((p) => [...p]) };
}

// Partial eraser: only the parts of the lines under the brush (frame points, radius r) go; a line
// is cut where the brush starts and ends touching it
export function eraseSpans(shapes, brush, r, map) {
  const out = [];
  for (const sh of shapes) {
    if (sh.points.length < 2) { out.push(sh); continue; }
    const pl = polyline(sh, map), n = segs(sh);
    const ins = pl.map((p) => brush.some((b) => Math.hypot(p.x - b.x, p.y - b.y) <= r));
    if (!ins.some(Boolean)) { out.push(sh); continue; }
    if (ins.every(Boolean)) continue;
    // the kept runs of samples, as parameter ranges (cut halfway between a kept and an erased sample)
    const runs = [];
    let k = 0;
    while (k < pl.length) {
      if (ins[k]) { k++; continue; }
      const a = k;
      while (k < pl.length && !ins[k]) k++;
      const s0 = a === 0 ? pl[0].s : (pl[a - 1].s + pl[a].s) / 2;
      const s1 = k === pl.length ? pl[pl.length - 1].s : (pl[k - 1].s + pl[k].s) / 2;
      runs.push([s0, s1]);
    }
    // a closed shape: the run at the end goes on into the run at the start
    if (!sh.open && runs.length > 1 && !ins[0] && !ins[pl.length - 1]) {
      const first = runs.shift(), last = runs.pop();
      runs.push([last[0], first[1] + n]);
    }
    for (const [s0, s1] of runs) if (s1 - s0 > 1e-3) {
      const p = piece(sh, s0, s1);
      if (p.points.length >= 2) out.push(p);
    }
  }
  return out;
}

// Is frame point f inside a polyline (closed)?
function inside(pl, f) {
  let c = false;
  for (let i = 0, j = pl.length - 1; i < pl.length; j = i++) {
    const a = pl[i], b = pl[j];
    if ((a.y > f.y) !== (b.y > f.y) && f.x < ((b.x - a.x) * (f.y - a.y)) / (b.y - a.y) + a.x) c = !c;
  }
  return c;
}

// The shape at frame point f: on its line (within tol px) or inside it (closed); the topmost (last)
export function hitShape(shapes, f, tol, map) {
  for (let i = shapes.length - 1; i >= 0; i--) {
    const sh = shapes[i];
    if (sh.points.length < 2) continue;
    const pl = polyline(sh, map);
    if (pl.some((p) => Math.hypot(p.x - f.x, p.y - f.y) <= tol) || (!sh.open && inside(pl, f))) return i;
  }
  return -1;
}

// The shapes of a group (or just i when it has none)
export const withGroup = (shapes, i) => (shapes[i] && shapes[i].group
  ? shapes.map((s, k) => (s.group === shapes[i].group ? k : -1)).filter((k) => k >= 0) : [i]);

// The box (frame px) around shapes idx
export function boxOf(shapes, idx, map) {
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const i of idx) for (const p of polyline(shapes[i], map)) {
    x0 = Math.min(x0, p.x); y0 = Math.min(y0, p.y); x1 = Math.max(x1, p.x); y1 = Math.max(y1, p.y);
  }
  return x0 === Infinity ? null : { x0, y0, x1, y1 };
}

// Shapes idx moved by f(x, y) → {x, y} (frame px), their nodes and their handles (a handle is a
// point too: its end is moved and the offset taken again). The others stay.
export function transformShapes(shapes, idx, f, map, back) {
  const r5 = (v) => +v.toFixed(5);
  const mv = (u, v) => { const p = map(u, v), q = f(p.x, p.y); return back(q.x, q.y); };
  return shapes.map((sh, i) => {
    if (!idx.includes(i)) return sh;
    return { ...sh, points: sh.points.map((p) => {
      const [u, v] = mv(p[0], p[1]);
      const q = [r5(u), r5(v), ...p.slice(2)];
      if (p.length >= 7) {
        const [iu, iv] = mv(p[0] + p[3], p[1] + p[4]), [ou, ov] = mv(p[0] + p[5], p[1] + p[6]);
        q[3] = r5(iu - u); q[4] = r5(iv - v); q[5] = r5(ou - u); q[6] = r5(ov - v);
      }
      return q;
    }) };
  });
}
