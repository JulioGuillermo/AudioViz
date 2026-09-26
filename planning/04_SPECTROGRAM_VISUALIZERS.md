# Motores de Visualización y Espectrogramas Estéreo Simétricos

## 1. Principio de Simetría Estéreo (L vs R)

Para reflejar la espacialidad real de la mezcla musical:
- El **Canal Izquierdo (L)** comanda la mitad izquierda de la pantalla o del visualizador.
- El **Canal Derecho (R)** comanda la mitad derecha.
- Ambos flujos se alimentan de sus propios espectros FFT de forma independiente, haciendo evidente cuando un instrumento o efecto pasa de un auricular/altavoz al otro (*pan / estéreo ping-pong*).

---

## 2. Tipos de Visualizadores Diseñados

### A. Espectrograma Circular Central (Radial Center)
Un anillo o círculo centrado en el cuadro:
- **Disposición Angular**:
  - **Semicírculo Izquierdo ($90^\circ$ a $270^\circ$)**: Proyecta las frecuencias del Canal L. Desde el polo superior o inferior hacia los lados, mapeando graves cerca del polo y agudos hacia el ecuador (o viceversa).
  - **Semicírculo Derecho ($-90^\circ$ a $90^\circ$)**: Proyecta las frecuencias del Canal R de manera exactamente simétrica.
- **Variantes de Estilo**:
  1. *Radial Bars*: Barras que irradian hacia afuera (o hacia adentro) desde el radio base $R_0$.
  2. *Smooth Bezier Ring*: Un trazado continuo y cerrado que deforma una corona circular como una membrana viva.
  3. *Filled Polygon with Glow*: Relleno semitransparente interior con degradado neón y contorno iluminado.
  4. *Double Ring (Bass Pulse)*: Anillo interior que pulsa con los graves y anillo exterior que baila con el espectro completo.

```
                  Polo Superior (Graves)
                       . - ~ ~ - .
                   .-'      |      '-.
                .-'    L    |    R    '-.
              .'            |            '.
             /              |              \
            |    Canal L    (C)   Canal R   |
             \              |              /
              '.            |            .'
                '-.         |         .-'
                   '-. _    |    _ .-'
                         ~ - - ~
                  Polo Inferior (Agudos)
```

### B. Espectrograma Horizontal Lineal (Bottom / Center)
- **Posición**: Ubicado usualmente en la base de la pantalla ($y \approx 0.85 \cdot H$) o centrado horizontalmente.
- **Modos de Simetría**:
  1. *Center-Out Mirrored*: El centro de la pantalla es el punto de inicio ($x = W/2$). El Canal L se despliega hacia la izquierda y el Canal R hacia la derecha. Las frecuencias graves parten del centro, creando un impacto visual expansivo en cada bombo.
  2. *Dual Channel Bottom Split*: Pantalla dividida en dos mitades simétricas:
     - $[0 \dots W/2]$: Canal L (Graves en el centro o en el extremo).
     - $[W/2 \dots W]$: Canal R.
  3. *Reflected Mirror*: Barras que crecen hacia arriba y se reflejan atenuadas hacia abajo con transparencia (*Water Reflection Effect*).

### C. Espectrograma Vertical Lateral (Edge Inward)
- Las barras se alinean verticalmente en los márgenes izquierdo y derecho de la pantalla:
  - Margen izquierdo: Canal L proyectándose hacia el centro.
  - Margen derecho: Canal R proyectándose hacia el centro.
- Deja todo el centro de la imagen libre para destacar al sujeto o personaje principal.

### D. Osciloscopio Estéreo (Waveform Neon Lines)
- Dibuja la forma de onda cruda en el dominio del tiempo con atenuación y persistencia:
  - Trazado de línea brillante con desenfoque gaussiano superpuesto para efecto neón (*bloom*).
  - Dos líneas entrelazadas (color Cyan para L, Magenta para R, mezclándose en Blanco aditivo en el centro).

---

## 3. Paletas de Color y Efecto Glow (Resplandor)

- **Gradientes de Color Preconfigurados**:
  - *Cyberpunk*: Cyan `#00f0ff` $\rightarrow$ Deep Purple `#7928ca` $\rightarrow$ Hot Pink `#ff007f`.
  - *Fire / Inferno*: Dark Red `#8b0000` $\rightarrow$ Orange `#ff4500` $\rightarrow$ Gold Yellow `#ffd700`.
  - *Synthwave / Sunset*: Orange Sunset `#ff5e36` $\rightarrow$ Violet `#9b51e0` $\rightarrow$ Electric Blue `#00c9ff`.
  - *Matrix / Toxic*: Dark Emerald `#004d40` $\rightarrow$ Neon Green `#00e676` $\rightarrow$ Electric Lime `#ccff00`.
  - *Monochrome Pure*: Blanco `#ffffff` con opacidades decrecientes y brillo suave.
  - *Custom Hex Palette*: Definible por el usuario en el archivo de configuración.
- **Renderizado Anti-Aliased & Glow**:
  - Renderizado vectorial de alta precisión con curvas suaves.
  - El resplandor neón se simula mediante pases múltiples con radio creciente y opacidad decreciente (*Multi-pass Alpha Convolution*).
