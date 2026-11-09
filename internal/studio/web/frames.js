import { useEffect, useRef, useState } from './vendor/preact-htm.js';

// Frames of the server (/api/frame) drawn into a canvas, at the given time: while playing,
// requested ahead by the measured latency with several in flight; when still, the exact frame
// (one request at a time). Returns { ms, fps } for the info line. Shared by the preview and the
// editor.
export function useFrameStream(canvas, { meta, time, scale, rev, playing, view = '' }) {
  // inflight: requests in progress; lat: average latency (ms); shownF: last drawn frame
  // epoch: bumped at every jump / play / stop; an answer of an older one is never drawn (a late
  // frame from before a jump backwards would otherwise stall the playback until time caught up)
  const st = useRef({ epoch: 0, inflight: 0, lat: 40, shownKey: null, shownF: -1, shownSeq: 0, seq: 0, lastReq: '', lastT: 0 });
  const [info, setInfo] = useState({ ms: 0, fps: 0, layers: [], texts: [] });
  // a still frame that failed or timed out is asked again (one that takes long, a simulated gas
  // worked out up to there, goes on where the server got to)
  const [retry, setRetry] = useState(0);
  const geo = useRef([]); // where the overlay clips are in the frame on screen (X-Layers)
  const tgeo = useRef([]); // and the fixed texts (X-Texts)
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
    const seq = ++s.seq, epoch = s.epoch;
    s.inflight++;
    s.lastReq = key;
    const t0 = performance.now();
    // a request that never answers must not block the next ones
    const ac = new AbortController();
    const to = setTimeout(() => ac.abort(), 6000);
    fetch(`/api/frame?f=${f}&scale=${scale}${view ? '&view=' + view : ''}`, { signal: ac.signal })
      .then((r) => {
        if (!r.ok) throw new Error(r.statusText);
        const xl = r.headers.get('X-Layers'), xt = r.headers.get('X-Texts');
        return r.blob().then((b) => createImageBitmap(b)).then((bmp) => ({ bmp, xl, xt }));
      })
      .then(({ bmp, xl, xt }) => {
        s.lat = s.lat * 0.8 + (performance.now() - t0) * 0.2;
        // Only draw if it is newer than what is on screen
        if (seq > s.shownSeq && epoch === s.epoch) {
          draw(bmp); s.shownSeq = seq; s.shownF = f; s.shownKey = key;
          try { geo.current = xl ? JSON.parse(xl) : []; } catch (e) { geo.current = []; }
          try { tgeo.current = xt ? JSON.parse(xt) : []; } catch (e) { tgeo.current = []; }
        }
        const now = performance.now();
        fpsWin.current = [...fpsWin.current.filter((x) => now - x < 1000), now];
        setInfo({ ms: Math.round(s.lat), fps: fpsWin.current.length, layers: geo.current, texts: tgeo.current });
      })
      .catch(() => {
        if (s.lastReq === key) s.lastReq = '';
        if (s.retryKey !== key) { s.retryKey = key; s.retryN = 0; }
        if (!s.playing && epoch === s.epoch && s.retryN++ < 30) setTimeout(() => setRetry((n) => n + 1), 250);
      })
      .finally(() => { clearTimeout(to); s.inflight--; });
  };

  // (before the one below: effects run in order, so a play / stop starts the new epoch first)
  useEffect(() => { const s = st.current; s.epoch++; s.shownF = -1; s.lastReq = ''; }, [playing]);
  useEffect(() => {
    const s = st.current;
    s.playing = playing;
    const fps = meta.fps;
    // A jump (seeking while playing) restarts the frame sequence, otherwise a jump backwards
    // would never be requested
    // (while still, frames of a drag keep being drawn in order: no new epoch)
    const jump = () => { if (playing) s.epoch++; s.shownF = -1; s.lastReq = ''; };
    if (time < s.lastT - 0.05 || time > s.lastT + 1.0) jump();
    s.lastT = time;
    if (playing) {
      // Request ahead by the measured latency, with several requests in flight
      const f = Math.min(meta.frames - 1, Math.round((time + s.lat / 1000) * fps));
      // what is on screen is far ahead of the cursor (should not happen): start again from here
      if (s.shownF > f + fps) jump();
      const key = `${f}|${scale}|${rev}|${view}`;
      if (s.inflight < 3 && key !== s.lastReq && f > s.shownF) request(f, key);
      return;
    }
    const f = Math.min(meta.frames - 1, Math.max(0, Math.round(time * fps)));
    const key = `${f}|${scale}|${rev}|${view}`;
    if (key === s.shownKey) return;
    if (s.inflight === 0) { request(f, key); return; }
    // One in progress: retry when it finishes
    const id = setInterval(() => {
      if (st.current.inflight === 0) { clearInterval(id); if (st.current.shownKey !== key) request(f, key); }
    }, 15);
    return () => clearInterval(id);
  }, [time, scale, rev, meta.frames, playing, view, retry]);

  return info;
}
