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

## Coming next

Values that change per particle, letter or pixel (`route.name`, `index`, per-particle `rand()`)
run on the GPU's interpreter (the same bytecode); colors (`rgb()`, `rgba()`, `hsv()`) and on/off
properties.
