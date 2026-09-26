#!/bin/bash
# Genera un .lrc con tiempo por palabra: run.sh cancion.flac letra.txt salida.lrc [opciones]
# La primera vez crea el entorno (torch CPU + demucs + stable-ts) en tools/lyrics/.venv
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
if [ ! -x "$DIR/.venv/bin/python" ]; then
  uv venv -q -p 3.12 "$DIR/.venv"
  VIRTUAL_ENV="$DIR/.venv" uv pip install -q --index-url https://download.pytorch.org/whl/cpu torch==2.5.1 torchaudio==2.5.1
  VIRTUAL_ENV="$DIR/.venv" uv pip install -q demucs stable-ts soundfile
fi
exec "$DIR/.venv/bin/python" -W ignore "$DIR/align.py" "$@"
