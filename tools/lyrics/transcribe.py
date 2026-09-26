#!/usr/bin/env python3
"""Transcribe (sin letra) un tramo de audio con tiempo por palabra, para comprobar qué se canta y cuándo.

  tools/lyrics/py transcribe.py tools/lyrics/cache/vocals.wav 235 270 [--model large-v3] [--lang en]
"""
import argparse
import os

import stable_whisper
import torch
from stable_whisper.audio import load_audio

ap = argparse.ArgumentParser()
ap.add_argument("audio")
ap.add_argument("start", type=float)
ap.add_argument("end", type=float, nargs="?")
ap.add_argument("--model", default="large-v3")
ap.add_argument("--lang", default="en")
args = ap.parse_args()

torch.set_num_threads(os.cpu_count() or 4)
sr = 16000
audio = load_audio(args.audio)
audio = audio[int(args.start * sr): int(args.end * sr) if args.end else None]
model = stable_whisper.load_model(args.model, device="cpu")
result = model.transcribe(audio, language=args.lang, word_timestamps=True, vad=False)


def ts(t):
    return f"{int(t) // 60:02d}:{t % 60:05.2f}"


for seg in result.segments:
    for w in seg.words:
        s, e = args.start + w.start, args.start + w.end
        print(f"{ts(s)} - {ts(e)}  {w.word.strip()}")
