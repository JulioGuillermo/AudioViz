#!/usr/bin/env python3
"""Muestra la energía de la voz segundo a segundo: dónde se canta y dónde hay silencio.

  tools/lyrics/py energy.py tools/lyrics/cache/vocals.wav 100 132
"""
import sys

import numpy as np
import soundfile as sf

path = sys.argv[1]
a = float(sys.argv[2]) if len(sys.argv) > 2 else 0
b = float(sys.argv[3]) if len(sys.argv) > 3 else None
step = float(sys.argv[4]) if len(sys.argv) > 4 else 1.0
x, sr = sf.read(path)
if x.ndim > 1:
    x = x.mean(axis=1)
b = b if b is not None else len(x) / sr
for t in np.arange(a, b, step):
    seg = x[int(t * sr):int((t + step) * sr)]
    r = float(np.sqrt((seg ** 2).mean())) if len(seg) else 0.0
    print(f"{int(t) // 60}:{t % 60:04.1f} {r:.3f} " + "#" * int(r * 300))
