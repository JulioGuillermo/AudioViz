#!/usr/bin/env python3
"""
Genera un .lrc con tiempo por palabra a partir del audio y la letra en texto.
Herramienta aparte del binario (no hace falta para renderizar).

  1. Limpia la letra (formato Suno: [Verse], [indicaciones de estilo]...)
  2. Separa la voz con Demucs (mejora mucho el alineado con música densa)
  3. Alinea el texto contra la voz con stable-ts (Whisper) -> tiempo por palabra
  4. Escribe LRC mejorado: [mm:ss.xx]<mm:ss.xx>palabra <mm:ss.xx>palabra ... <fin>

Uso:
  tools/lyrics/run.sh cancion.flac letra.txt salida.lrc [--model large-v3] [--lang en]
"""
import argparse
import os
import re
import subprocess
import sys
import tempfile


def clean_lyrics(text: str) -> list[str]:
    lines = []
    for raw in text.splitlines():
        s = raw.strip()
        if not s:
            continue
        # Etiquetas Suno/Udio: [Chorus], [Soft piano...], (whispered)...
        if re.fullmatch(r"\[[^\]]*\]", s) or re.fullmatch(r"\([^)]*\)", s):
            continue
        s = re.sub(r"\[[^\]]*\]", "", s).strip()
        if s:
            lines.append(s)
    return lines


def norm(w: str) -> str:
    return re.sub(r"[^\w']", "", w.lower().replace("’", "'"))


def ts(t: float) -> str:
    t = max(t, 0.0)
    m = int(t // 60)
    return f"{m:02d}:{t - 60 * m:05.2f}"


def separate_vocals(audio: str, workdir: str) -> str:
    print("🎙️  Separando la voz con Demucs (htdemucs)...", flush=True)
    subprocess.run([sys.executable, "-m", "demucs", "--two-stems", "vocals", "-n", "htdemucs",
                    "-o", workdir, audio], check=True)
    name = os.path.splitext(os.path.basename(audio))[0]
    path = os.path.join(workdir, "htdemucs", name, "vocals.wav")
    if not os.path.exists(path):
        raise SystemExit(f"Demucs no generó {path}")
    return path


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("audio")
    ap.add_argument("lyrics")
    ap.add_argument("out")
    ap.add_argument("--model", default="large-v3")
    ap.add_argument("--lang", default="en")
    ap.add_argument("--no-separate", action="store_true", help="no separar la voz")
    ap.add_argument("--vocals", help="usar este archivo de voz ya separado")
    args = ap.parse_args()

    with open(args.lyrics, encoding="utf-8") as f:
        lines = clean_lyrics(f.read())
    print(f"📝 {len(lines)} líneas de letra", flush=True)

    work = tempfile.mkdtemp(prefix="lyrics_")
    vocals = args.vocals or (args.audio if args.no_separate else separate_vocals(args.audio, work))

    import stable_whisper
    import torch
    torch.set_num_threads(os.cpu_count() or 4)
    print(f"🧠 Cargando Whisper {args.model} y alineando...", flush=True)
    model = stable_whisper.load_model(args.model, device="cpu")
    text = "\n".join(lines)
    result = model.align(vocals, text, language=args.lang, original_split=True)

    words = [w for seg in result.segments for w in seg.words]
    # Reasignar las palabras alineadas a las líneas originales (mismo orden de palabras)
    out = []
    wi = 0
    for line in lines:
        toks = line.split()
        timed = []
        for tok in toks:
            # buscar la siguiente palabra alineada que coincide (tolerante a signos)
            j = wi
            while j < len(words) and norm(words[j].word) != norm(tok) and j < wi + 3:
                j += 1
            if j < len(words) and norm(words[j].word) == norm(tok):
                wi = j
            w = words[min(wi, len(words) - 1)]
            timed.append((tok, w.start, w.end))
            wi += 1
        out.append(timed)

    # Reparar líneas mal alineadas (palabras amontonadas con duración ~0): se vuelven a alinear
    # solo en el hueco de audio entre la línea buena anterior y la siguiente
    from stable_whisper.audio import load_audio
    import numpy as np
    sr = 16000
    audio16 = load_audio(vocals)
    fr = sr // 20  # tramos de 50 ms
    nfr = len(audio16) // fr
    rms = np.sqrt((audio16[: nfr * fr].reshape(nfr, fr) ** 2).mean(axis=1))
    silence_thr = 0.06 * float(np.percentile(rms, 95))

    def longest_silence(a, b):
        """Mayor tramo continuo (s) sin voz entre a y b"""
        seg = rms[int(a * 20): int(b * 20)] < silence_thr
        best = run = 0
        for v in seg:
            run = run + 1 if v else 0
            best = max(best, run)
        return best / 20

    def bad(timed):
        short = sum(1 for _, s, e in timed if e - s < 0.07)
        span = timed[-1][2] - timed[0][1]
        if short >= max(2, len(timed) // 2) or span < 0.12 * len(timed):
            return True
        # Un instrumental en mitad de la línea: la alineación se ha "estirado" sobre un silencio real
        for k in range(len(timed) - 1):
            a, b = timed[k][2], timed[k + 1][1]
            if b - a > 3.0 and longest_silence(a, b) >= 3.0:
                return True
        return False

    i = 0
    while i < len(out):
        if not bad(out[i]):
            i += 1
            continue
        j = i
        while j < len(out) and bad(out[j]):
            j += 1
        t0 = out[i - 1][-1][2] if i > 0 else 0.0
        t1 = out[j][0][1] if j < len(out) else None
        seg = audio16[int(t0 * sr): int(t1 * sr) if t1 else None]
        # Saltar el silencio inicial del hueco (p. ej. un instrumental) usando la energía de la voz
        n = len(seg) // fr
        if n > 0:
            r = np.sqrt((seg[: n * fr].reshape(n, fr) ** 2).mean(axis=1))
            voiced = r > silence_thr
            first = int(np.argmax(voiced))
            # Final: primer silencio de >= 2 s tras la voz, siempre que el tramo con voz
            # dé para ~0.25 s por palabra (si no, se amplía al siguiente tramo)
            nwords = sum(len(out[li]) for li in range(i, j))
            end, run, vo = n, 0, 0
            for f in range(first, n):
                if voiced[f]:
                    run, vo = 0, vo + 1
                else:
                    run += 1
                    if run >= 40 and vo / 20 >= 0.25 * nwords:
                        end = f - run + 12  # 0.6 s de margen tras la voz
                        break
            skip = max(0, first - 4) * fr  # 0.2 s de margen
            seg = seg[skip: end * fr]
            t0 += skip / sr
        print(f"🔧 Realineando líneas {i + 1}-{j} en {ts(t0)}–{ts(t1) if t1 else 'fin'}", flush=True)
        sub = model.align(seg, "\n".join(lines[i:j]), language=args.lang, original_split=True)
        sw = [w for sg in sub.segments for w in sg.words]
        k = 0
        for li in range(i, j):
            new = []
            for tok, _, _ in out[li]:
                w = sw[min(k, len(sw) - 1)]
                new.append((tok, t0 + w.start, t0 + w.end))
                k += 1
            out[li] = new
        i = j

    # Asegurar tiempos monótonos
    last = 0.0
    for timed in out:
        for k, (tok, s, e) in enumerate(timed):
            s = max(s, last)
            e = max(e, s + 0.05)
            timed[k] = (tok, s, e)
            last = s

    with open(args.out, "w", encoding="utf-8") as f:
        f.write("[re:audioviz tools/lyrics/align.py]\n")
        for timed in out:
            body = " ".join(f"<{ts(s)}>{tok}" for tok, s, _ in timed)
            f.write(f"[{ts(timed[0][1])}]{body} <{ts(timed[-1][2])}>\n")
    print(f"✅ Letra sincronizada escrita en {args.out}")


if __name__ == "__main__":
    main()
