# Letra sincronizada paso a paso

1. **Prepara la letra** en un `.txt` (el formato de Suno vale tal cual). Si una frase se repite al
   cantarla, escríbela tantas veces como se canta.
2. **Genera** el `.lrc`:
   - en el estudio: pista **Letra** → *Generar letra sincronizada…*, o arrastra el `.txt` a la ventana;
   - o en la terminal: `./audioviz lyrics "cancion.flac" letra.txt -o letra.lrc`
3. **Revisa**: reproduce en el estudio y mira la pista *Líneas de letra*. Para retocar una palabra,
   clic en su línea: el inspector muestra el inicio y el fin de cada palabra (el botón de mira pone el
   inicio en el cursor).
4. **Si todo va desplazado** lo mismo, usa *Desfase* en la pista Letra (`lyrics.offset`).
5. **Exporta**.

Detalles de cada paso, modos y precisión: [generar.md](generar.md).

## Ejemplo completo en terminal
```bash
cd ~/audio_bg
./audioviz lyrics "I didnt ask to be the devil.flac" "I didn't ask to be the devil.txt" -o letra.lrc
#  [ 13%] 3/5 Separar la voz   8% · 0:17 · quedan 3:35 · Demucs htdemucs · CPU
#  [ 79%] 4/5 Reconocer la voz 100% · 0:18 · 460 palabras reconocidas · Intel(R) Graphics (LNL)
#  [100%] 5/5 Alinear con la letra 100% · 60 líneas · 380 de 380 palabras reconocidas (100%)
./audioviz project.yaml --lyrics letra.lrc -p 15 --ss 110 -o prueba.mp4   # comprobar un tramo
```
