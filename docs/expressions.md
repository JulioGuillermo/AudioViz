# Expressions (advanced settings)

Any number of any element (a clip, a text, an emitter, a segment of a track) can be given by an
**expression** instead of only its slider: right-click the property → **Advanced settings…**.
The slider and its box stay what they are: the expression reads them as `value` and gives the
property's real value. A property with an expression shows an **ƒ** before its name and the value
it gives now under it; its slider goes grey when the expression doesn't use `value`. Right-click
also copies / pastes / removes an expression.

The dialog checks it as it is written (a mistake is underlined, with its line) and draws what it
gives over the element's time (or ±6 s around the cursor), the cursor in orange.

```yaml
      exprs:
        scale: value * (0.6 + lag(bass, 0.2))         # pushed by the bass, smoothed
        opacity: if(kick > 0.5 -> 1, 0.4)             # full on each kick
        x: $wob(value)                                # a function of the project
```

## The language

| | |
|---|---|
| Numbers | `2`, `0.5`; `+ - * / % ^` (`^` power), `( )` |
| Comparisons | `== != < <= > >=`, `&&` / `and`, `\|\|` / `or`, `!` / `not` (true is more than 0) |
| `nil` | nothing; counts as 0 in any operation. `a ?? b`: `b` when `a` is `nil` |
| `if` | `if(cond -> a, cond2 -> b, …, else)`: the first true; without an else, `nil` |
| Lines | one expression per line; `let x = …` names a step; the last line is the result; `ret(x)` gives a result at once; `#` a comment |

### What it reads

| | |
|---|---|
| `value` | the property's value from its slider (and its keyframes) |
| `clip_time`, `clip_progress` | the element's: seconds from its start, 0…1 of its length (a track's segment: until the next one) |
| `project_time`, `project_progress` | the whole project's: seconds from the song's start, 0…1 of the song (`time`, `progress` for short) |
| `frame`, `fps` | the frame number and the frames per second |
| `bass`, `mid`, `treble`, `volume`, `transient`, `beat` | the mix's bands (0…1, as *Reacts to*) |
| `drums`, `kick`, `snare`, `hats`, `bassline`, `vocals`, `other` | the separated instruments ([audio](audio.md)) |
| `bass@-0.5` | a band 0.5 s before (or after, with +) |
| `lag(bass, 0.3)` | smoothed over 0.3 s; `peak(kick, 1)`: the loudest of the last second |
| `path.name` | a named value of the motion path's nodes at its place ([node values](layers.md#values-on-the-nodes)) |
| `path.x`, `path.y`, `path.progress`, `path.angle` | where it is on its motion path: x, y (0…1 of the frame, as *Position*), how far along (0…1), its direction (degrees); an emitter's particles each where they are on their `route.` (the same four) or path; a text's letters each where they are. These names are not for the nodes' values |
| `pi`, `tau`, `e` | |

### Functions

Angles are in **degrees** (like the inspector): `sin cos tan asin acos atan atan2`; in radians
`rad_sin rad_cos …`; `deg(x)`, `rad(x)` convert. Also `abs sign floor ceil round fract sqrt exp
log pow mod min max clamp mix (lerp) smoothstep step`, `map(x, a, b, c, d)` (from a…b to c…d),
`noise(x)` and `noise2(x, y)` (smooth, 0…1), `rand()` (stable: the same each frame for the
element), `random()` (a new one each frame, the same in every render), `is_nil(x)`.

Something impossible (a division by 0, the root of a negative…) gives 0 there and the dialog says
so; the render still keeps the limits it needs (an opacity stays 0…1).

## The project's constants and functions

**Output → Functions and constants**: constants (`$amp = 0.08`) and functions, all with `$` (ours
have none). A function's body can go on the lines under it, indented:

```yaml
expressions:
  constants: { amp: 0.08 }
  functions: |
    $wob(x) = x + sin(project_time * 180) * $amp
    $pulse(x, strength) =
      if(strength <= 0 -> ret(x))
      let k = lag(kick, 0.2) * strength
      x * (1 + k)
```

A function can call other functions but not itself (there are no loops: an expression always
ends). An expression can also define its own functions before its last lines.

## Per particle

An emitter's **speed, life, size, size at the end, gravity, wind, turbulence, direction and
spread** can change per particle: an expression that reads `index` (the particle's number),
`count`, `rand()` / `random()` (each particle its own) or `route.name` (the route's value where
the particle is; `path.name` too when they travel along the motion path) is worked out for each
particle, on the GPU (the same bytecode, its own interpreter) and the CPU alike. The audio and the
rest are worked out once per frame. E.g. flames that grow where the route's `w` is big, each at
its own speed:

```yaml
      exprs:
        emitter.size: value * route.w
        emitter.speed: value * (0.6 + rand() * 0.8)
```

## Per letter

A fixed text's **size, rotation, position_x and position_y** can change per letter: an
expression that reads `index` (the letter's number), `count` (how many), `rand()` / `random()`
(each letter its own) or `path.name` (the motion path's value where the letter is) lays the text
out letter by letter and gives each its own: its size scales it, its rotation turns it and its
position moves it by how much they differ from the text's. E.g. a wave running through the
letters, each slightly turned:

```yaml
    exprs:
      position_y: value + sin(index * 40 + time * 200) * 0.03
      rotation: value + (rand() - 0.5) * 30
      size: value * (0.8 + 0.4 * index / count)
```

## Colors and on/off

A **color** property's expression is worked out once per channel (red, green, blue, opacity,
0…1): `value` is the color (`value * 0.5` darkens it — and halves its opacity), `value.r`,
`value.g`, `value.b`, `value.a` its channels; `rgb(r, g, b)` (keeping its opacity), `rgba(r, g, b,
a)`, `hsv(hue°, saturation, brightness)` give one. E.g. a color that goes red with the bass:
`mix(value, rgb(1, 0, 0), lag(bass, 0.2))`; a hue turning with the song: `hsv(time * 30, 0.8, 1)`.
The dialog shows it as a strip over time.

## Per letter or per pixel

Some properties can change across their element: in the dialog, *Worked out* picks how — for all
of it, per letter (texts), per pixel, or *automatic* (per pixel when the expression reads where
the pixel is; on a text, per letter when it reads `index`, `count`, `rand()` or — on a bent text —
its path's values; else for all of it).

- **A fixed text:** its color, outline color, glow color, shadow color, opacity, outline width,
  glow intensity and radius, shadow opacity and blur.
- **A clip** (image or video): its opacity, brightness, contrast, saturation, hue, and its chroma
  key's color, tolerance and softness.

They can read:

| | |
|---|---|
| `text_x`, `text_y` | where it is in the text's box, 0…1 (left → right, top → bottom) |
| `letter_x`, `letter_y` | where it is in its letter, 0…1 |
| `image_x`, `image_y` | where it is in a clip's image, 0…1 |
| `screen_x`, `screen_y` | where it is on the screen, 0…1 |
| `index`, `count`, `rand()` | a text's letter, how many there are, its random number |
| `path.progress`, `path.name`… | its path's values where it is (along the path) |
| `value` | what it would be there (a text's color: its gradient included) |

Per letter, a letter is read at its middle. Elsewhere (or worked out for all of it) these read
0.5. E.g. a rainbow across a text, moving: `hsv(text_x * 300 + time * 60, 0.9, 1)`; each letter
its own: `hsv(index / count * 360, 0.8, 1)`; fire from the bottom of each letter:
`mix(value, rgb(1, 0.3, 0), smoothstep(0.3, 1, letter_y))`; a glow growing along the text:
`4 + 16 * text_x` (glow radius); a clip fading in from its left edge: `smoothstep(0, 0.4, image_x)`
(opacity).

An **on/off** property is on when its expression is more than 0: `bass > 0.6` turns it on with the
bass.
