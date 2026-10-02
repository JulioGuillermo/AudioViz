# Synced lyrics step by step

1. **Prepare the lyrics** in a `.txt` (Suno format works as is). If a phrase is repeated when
   sung, write it as many times as it is sung.
2. **Generate** the `.lrc`:
   - in the studio: **Lyrics** track → *Generate synced lyrics…*, or drag the `.txt` onto the window;
   - or in the terminal: `./audioviz lyrics "song.flac" lyrics.txt -o lyrics.lrc`
3. **Review**: play it in the studio and watch the *Lyric lines* track. To tweak a word,
   click its line: the inspector shows the start and end of each word (the crosshair button sets the
   start at the cursor).
4. **If everything is shifted** by the same amount, use *Offset* in the Lyrics track (`lyrics.offset`).
5. **Export**.

Details on each step, the modes and accuracy: [generate.md](generate.md).

## Complete terminal example
```bash
cd ~/audio_bg
./audioviz lyrics "I didnt ask to be the devil.flac" "I didn't ask to be the devil.txt" -o lyrics.lrc
#  [ 13%] 3/5 Separate the vocals   8% · 0:17 · 3:35 left · Demucs htdemucs · CPU
#  [ 79%] 4/5 Recognize the vocals 100% · 0:18 · 460 words recognized · Intel(R) Graphics (LNL)
#  [100%] 5/5 Align with the lyrics 100% · 60 lines · 380 of 380 words recognized (100%)
./audioviz project.yaml --lyrics lyrics.lrc -p 15 --ss 110 -o test.mp4   # check a segment
```
