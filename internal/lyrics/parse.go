// Package lyrics carga letras sincronizadas (.srt, .lrc y LRC mejorado con tiempo por palabra)
// y las prepara para dibujarlas animadas.
package lyrics

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Word struct {
	Text       string
	Start, End float64
}

type Line struct {
	Start, End float64
	Words      []Word
	// WordTimed indica que los tiempos de palabra vienen del archivo (LRC mejorado)
	WordTimed bool
}

// Load lee un .srt o .lrc. offset (s) se suma a todos los tiempos.
func Load(path string, offset float64) ([]Line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer la letra '%s': %w", path, err)
	}
	text := strings.TrimPrefix(string(data), "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")

	var lines []Line
	switch strings.ToLower(filepath.Ext(path)) {
	case ".srt":
		lines, err = parseSRT(text)
	case ".lrc":
		lines, err = parseLRC(text)
	default:
		if strings.Contains(text, "-->") {
			lines, err = parseSRT(text)
		} else {
			lines, err = parseLRC(text)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("la letra '%s' no contiene líneas con tiempo", path)
	}
	for i := range lines {
		l := &lines[i]
		l.Start += offset
		l.End += offset
		for j := range l.Words {
			l.Words[j].Start += offset
			l.Words[j].End += offset
		}
	}
	return lines, nil
}

var srtTime = regexp.MustCompile(`(\d+):(\d+):(\d+)[,.](\d+)\s*-->\s*(\d+):(\d+):(\d+)[,.](\d+)`)
var tagRe = regexp.MustCompile(`<[^>]*>|\{[^}]*\}`)

func hmsms(h, m, s, ms string) float64 {
	hv, _ := strconv.Atoi(h)
	mv, _ := strconv.Atoi(m)
	sv, _ := strconv.Atoi(s)
	frac, _ := strconv.ParseFloat("0."+ms, 64)
	return float64(hv*3600+mv*60+sv) + frac
}

func parseSRT(text string) ([]Line, error) {
	var out []Line
	for _, block := range strings.Split(text, "\n\n") {
		rows := strings.Split(strings.TrimSpace(block), "\n")
		for i, r := range rows {
			m := srtTime.FindStringSubmatch(r)
			if m == nil {
				continue
			}
			content := strings.Join(rows[i+1:], " ")
			content = strings.TrimSpace(tagRe.ReplaceAllString(content, ""))
			if content == "" {
				break
			}
			l := Line{Start: hmsms(m[1], m[2], m[3], m[4]), End: hmsms(m[5], m[6], m[7], m[8])}
			for _, w := range strings.Fields(content) {
				l.Words = append(l.Words, Word{Text: w})
			}
			out = append(out, l)
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	for i := range out {
		distribute(&out[i], out[i].End)
	}
	return out, nil
}

var lrcTag = regexp.MustCompile(`^\[(\d+):(\d+(?:[.:]\d+)?)\]`)
var lrcMeta = regexp.MustCompile(`^\[([a-zA-Z]+):([^\]]*)\]`)
var wordTag = regexp.MustCompile(`<(\d+):(\d+(?:[.:]\d+)?)>`)

func lrcSeconds(m, s string) float64 {
	mv, _ := strconv.Atoi(m)
	s = strings.Replace(s, ":", ".", 1)
	sv, _ := strconv.ParseFloat(s, 64)
	return float64(mv)*60 + sv
}

func parseLRC(text string) ([]Line, error) {
	var out []Line
	globalOffset := 0.0
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		row := strings.TrimSpace(sc.Text())
		if row == "" {
			continue
		}
		// Una línea puede tener varias marcas de tiempo: [00:10.00][01:20.00]texto
		var starts []float64
		for {
			m := lrcTag.FindStringSubmatch(row)
			if m == nil {
				break
			}
			starts = append(starts, lrcSeconds(m[1], m[2]))
			row = row[len(m[0]):]
		}
		if len(starts) == 0 {
			if m := lrcMeta.FindStringSubmatch(row); m != nil && strings.EqualFold(m[1], "offset") {
				if ms, err := strconv.ParseFloat(strings.TrimSpace(m[2]), 64); err == nil {
					// En LRC un offset positivo adelanta la letra
					globalOffset = -ms / 1000
				}
			}
			continue
		}
		for _, st := range starts {
			l := parseLRCBody(row, st)
			if len(l.Words) > 0 || l.End > 0 {
				out = append(out, l)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start < out[j].Start })

	// Líneas vacías con tiempo marcan el final de la anterior
	var res []Line
	for i := range out {
		if len(out[i].Words) == 0 {
			if len(res) > 0 && res[len(res)-1].End == 0 {
				res[len(res)-1].End = out[i].Start
			}
			continue
		}
		res = append(res, out[i])
	}
	for i := range res {
		l := &res[i]
		next := l.Start + 8
		if i+1 < len(res) {
			next = res[i+1].Start
		}
		if l.End == 0 || l.End > next {
			// Sin final explícito: hasta la siguiente línea, pero sin quedarse eternamente
			// durante un solo instrumental
			est := l.Start + 1.5 + 0.45*float64(len(l.Words))
			if l.WordTimed {
				est = l.Words[len(l.Words)-1].End + 1.2
			}
			l.End = min(next, max(est, l.Start+1))
		}
		if !l.WordTimed {
			distribute(l, l.End)
		}
		for j := range l.Words {
			l.Words[j].Start += globalOffset
			l.Words[j].End += globalOffset
		}
		l.Start += globalOffset
		l.End += globalOffset
	}
	return res, nil
}

// parseLRCBody separa las palabras; si hay marcas <mm:ss.xx> (LRC mejorado) se usan como tiempos
func parseLRCBody(body string, start float64) Line {
	l := Line{Start: start}
	locs := wordTag.FindAllStringSubmatchIndex(body, -1)
	if len(locs) == 0 {
		for _, w := range strings.Fields(body) {
			l.Words = append(l.Words, Word{Text: w})
		}
		return l
	}
	l.WordTimed = true
	// Texto antes de la primera marca: empieza con la línea
	type seg struct {
		t    float64
		text string
	}
	var segs []seg
	if pre := strings.TrimSpace(body[:locs[0][0]]); pre != "" {
		segs = append(segs, seg{start, pre})
	}
	for i, loc := range locs {
		t := lrcSeconds(body[loc[2]:loc[3]], body[loc[4]:loc[5]])
		end := len(body)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		segs = append(segs, seg{t, body[loc[1]:end]})
	}
	for i, s := range segs {
		words := strings.Fields(s.text)
		if len(words) == 0 {
			// Marca final sin texto: fin de la palabra anterior
			if n := len(l.Words); n > 0 {
				l.Words[n-1].End = s.t
				l.End = s.t
			}
			continue
		}
		segEnd := s.t + 0.6
		if i+1 < len(segs) {
			segEnd = segs[i+1].t
		}
		// Varias palabras bajo una misma marca: se reparten
		step := (segEnd - s.t) / float64(len(words))
		for k, w := range words {
			l.Words = append(l.Words, Word{Text: w, Start: s.t + step*float64(k), End: s.t + step*float64(k+1)})
		}
	}
	return l
}

// distribute reparte los tiempos de palabra según su longitud cuando el archivo solo da tiempos por línea
func distribute(l *Line, end float64) {
	if len(l.Words) == 0 {
		return
	}
	dur := (end - l.Start) * 0.9
	if dur <= 0 {
		dur = 0.5
	}
	total := 0.0
	for _, w := range l.Words {
		total += float64(utf8.RuneCountInString(w.Text) + 1)
	}
	t := l.Start
	for i := range l.Words {
		d := dur * float64(utf8.RuneCountInString(l.Words[i].Text)+1) / total
		l.Words[i].Start, l.Words[i].End = t, t+d
		t += d
	}
}
