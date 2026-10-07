import { useEffect, useRef, useState } from './vendor/preact-htm.js';

// Frames of the server (/api/frame) drawn into a canvas, at the given time: while playing,
// requested ahead by the measured latency with several in flight; when still, the exact frame
// (one request at a time). Returns { ms, fps } for the info line. Shared by the preview and the
// editor.
export function useFrameStream(canvas, { meta, time, scale, rev, playing }) {
  // inflight: requests in progress; lat: average latency (ms); shownF: last drawn frame
  const st = useRef({ inflight: 0, lat: 40, shownKey: null, shownF: -1, shownSeq: 0, seq: 0, lastReq: '', lastT: 0 });
  const [info, setInfo] = useState({ ms: 0, fps: 0, layers: [] });
  const geo = useRef([]); // where the overlay clips are in the frame on screen (X-Layers)
  const fpsWin = useRef([]);

  const draw = (bmp) => {
    const c = canvas.current;
    if (!c) return;
    if (c.width !== bmp.width || c.height !== bmp.height) { c.width = bmp.width; c.height = bmp.height; }
    c.getContext('2d').drawImage(bmp, 0, 0);
    bmp.close && bmp.close();
  };

  const request = (f, key) => {
    const s = st.current;
    const seq = ++s.seq;
    s.inflight++;
    s.lastReq = key;
    const t0 = performance.now();
    fetch(`/api/frame?f=${f}&scale=${scale}`)
      .then((r) => {
        if (!r.ok) throw new Error(r.statusText);
        const xl = r.headers.get('X-Layers');
        return r.blob().then((b) => createImageBitmap(b)).then((bmp) => ({ bmp, xl }));
      })
      .then(({ bmp, xl }) => {
        s.lat = s.lat * 0.8 + (performance.now() - t0) * 0.2;
        // Only draw if it is newer than what is on screen
        if (seq > s.shownSeq) {
          draw(bmp); s.shownSeq = seq; s.shownF = f; s.shownKey = key;
          try { geo.current = xl ? JSON.parse(xl) : []; } catch (e) { geo.current = []; }
        }
        const now = performance.now();
        fpsWin.current = [...fpsWin.current.filter((x) => now - x < 1000), now];
        setInfo({ ms: Math.round(s.lat), fps: fpsWin.current.length, layers: geo.current });
      })
      .catch(() => {})
      .finally(() => { s.inflight--; });
  };

  useEffect(() => {
    const s = st.current;
    const fps = meta.fps;
    // A jump (seeking while playing) restarts the frame sequence, otherwise a jump backwards
    // would never be requested
    if (time < s.lastT - 0.05 || time > s.lastT + 1.0) { s.shownF = -1; s.lastReq = ''; }
    s.lastT = time;
    if (playing) {
      // Request ahead by the measured latency, with several requests in flight
      const f = Math.min(meta.frames - 1, Math.round((time + s.lat / 1000) * fps));
      const key = `${f}|${scale}|${rev}`;
      if (s.inflight < 3 && key !== s.lastReq && f > s.shownF) request(f, key);
      return;
    }
    const f = Math.min(meta.frames - 1, Math.max(0, Math.round(time * fps)));
    const key = `${f}|${scale}|${rev}`;
    if (key === s.shownKey) return;
    if (s.inflight === 0) { request(f, key); return; }
    // One in progress: retry when it finishes
    const id = setInterval(() => {
      if (st.current.inflight === 0) { clearInterval(id); if (st.current.shownKey !== key) request(f, key); }
    }, 15);
    return () => clearInterval(id);
  }, [time, scale, rev, meta.frames, playing]);

  useEffect(() => { if (!playing) st.current.shownF = -1; }, [playing]);
  return info;
}
