package depth

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// A PyTorch checkpoint (.pth: a zip with a pickled state dict and one file per storage) read
// without Python: a small pickle machine that knows only what a state dict of tensors needs.

type pthTensor struct {
	Shape []int
	Data  []float32 // row-major (contiguous), float32 (f16 / bf16 widened)
}

type pthGlobal struct{ mod, name string }

type pthStorage struct {
	key   string
	dtype string // FloatStorage, HalfStorage, BFloat16Storage
}

type pthReduce struct {
	fn   any
	args []any
}

type pthMark struct{}

// readPth: the tensors of a state dict saved by torch.save (the dict itself, or one under
// "state_dict" / "model")
func readPth(path string) (map[string]*pthTensor, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string]*zip.File{}
	var pkl *zip.File
	for _, f := range zr.File {
		files[f.Name] = f
		if strings.HasSuffix(f.Name, "/data.pkl") || f.Name == "data.pkl" {
			pkl = f
		}
	}
	if pkl == nil {
		return nil, errors.New("not a PyTorch checkpoint (no data.pkl)")
	}
	prefix := strings.TrimSuffix(pkl.Name, "data.pkl")
	rc, err := pkl.Open()
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return nil, err
	}
	root, err := unpickle(b)
	if err != nil {
		return nil, err
	}
	dict, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("checkpoint: a %T, not a dict", root)
	}
	for _, k := range []string{"state_dict", "model"} {
		if d, ok := dict[k].(map[string]any); ok {
			dict = d
		}
	}
	storages := map[string][]byte{}
	load := func(key string) ([]byte, error) {
		if b, ok := storages[key]; ok {
			return b, nil
		}
		f := files[prefix+"data/"+key]
		if f == nil {
			return nil, fmt.Errorf("checkpoint: storage %s missing", key)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err == nil {
			storages[key] = b
		}
		return b, err
	}
	out := map[string]*pthTensor{}
	for name, v := range dict {
		r, ok := v.(*pthReduce)
		if !ok {
			continue
		}
		g, ok := r.fn.(pthGlobal)
		if !ok || g.name != "_rebuild_tensor_v2" || len(r.args) < 4 {
			continue
		}
		st, ok := r.args[0].(*pthStorage)
		if !ok {
			return nil, fmt.Errorf("checkpoint: %s has no storage", name)
		}
		off := toInt(r.args[1])
		shape, stride := toInts(r.args[2]), toInts(r.args[3])
		n := 1
		for _, d := range shape {
			n *= d
		}
		// contiguous only (what a saved state dict holds)
		for i, s := len(shape)-1, 1; i >= 0; i-- {
			if shape[i] > 1 && stride[i] != s {
				return nil, fmt.Errorf("checkpoint: %s is not contiguous", name)
			}
			s *= shape[i]
		}
		raw, err := load(st.key)
		if err != nil {
			return nil, err
		}
		t := &pthTensor{Shape: shape, Data: make([]float32, n)}
		switch st.dtype {
		case "FloatStorage":
			if (off+n)*4 > len(raw) {
				return nil, fmt.Errorf("checkpoint: %s out of its storage", name)
			}
			for i := range t.Data {
				t.Data[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[(off+i)*4:]))
			}
		case "HalfStorage", "BFloat16Storage":
			if (off+n)*2 > len(raw) {
				return nil, fmt.Errorf("checkpoint: %s out of its storage", name)
			}
			for i := range t.Data {
				h := binary.LittleEndian.Uint16(raw[(off+i)*2:])
				if st.dtype == "HalfStorage" {
					t.Data[i] = halfToFloat(h)
				} else {
					t.Data[i] = math.Float32frombits(uint32(h) << 16)
				}
			}
		default:
			continue // (integer buffers: not weights)
		}
		out[name] = t
	}
	return out, nil
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	}
	return 0
}

func toInts(v any) []int {
	tu, _ := v.([]any)
	out := make([]int, len(tu))
	for i, x := range tu {
		out[i] = toInt(x)
	}
	return out
}

func halfToFloat(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := int(h>>10) & 0x1f
	frac := uint32(h & 0x3ff)
	switch {
	case exp == 0:
		if frac == 0 {
			return math.Float32frombits(sign)
		}
		f := float32(frac) / 1024 * float32(math.Pow(2, -14))
		if sign != 0 {
			f = -f
		}
		return f
	case exp == 31:
		return math.Float32frombits(sign | 0x7f800000 | frac<<13)
	}
	return math.Float32frombits(sign | uint32(exp-15+127)<<23 | frac<<13)
}

// unpickle runs the pickle opcodes a torch state dict uses
func unpickle(b []byte) (any, error) {
	var stack []any
	memo := map[int]any{}
	pos := 0
	need := func(n int) error {
		if pos+n > len(b) {
			return errors.New("pickle: truncated")
		}
		return nil
	}
	u32 := func() int { v := int(binary.LittleEndian.Uint32(b[pos:])); pos += 4; return v }
	pop := func() any { v := stack[len(stack)-1]; stack = stack[:len(stack)-1]; return v }
	popMark := func() []any {
		for i := len(stack) - 1; i >= 0; i-- {
			if _, ok := stack[i].(pthMark); ok {
				items := append([]any(nil), stack[i+1:]...)
				stack = stack[:i]
				return items
			}
		}
		return nil
	}
	line := func() string {
		i := pos
		for i < len(b) && b[i] != '\n' {
			i++
		}
		s := string(b[pos:i])
		pos = i + 1
		return s
	}
	setItems := func(d any, kv []any) {
		m, ok := d.(map[string]any)
		if !ok {
			return
		}
		for i := 0; i+1 < len(kv); i += 2 {
			if k, ok := kv[i].(string); ok {
				m[k] = kv[i+1]
			}
		}
	}
	for pos < len(b) {
		op := b[pos]
		pos++
		switch op {
		case 0x80: // PROTO
			pos++
		case 0x95: // FRAME
			pos += 8
		case '.': // STOP
			if len(stack) == 0 {
				return nil, errors.New("pickle: empty")
			}
			return pop(), nil
		case '(':
			stack = append(stack, pthMark{})
		case '}':
			stack = append(stack, map[string]any{})
		case ']', ')':
			stack = append(stack, []any{})
		case 'N':
			stack = append(stack, nil)
		case 0x88:
			stack = append(stack, true)
		case 0x89:
			stack = append(stack, false)
		case 'K':
			if err := need(1); err != nil {
				return nil, err
			}
			stack = append(stack, int(b[pos]))
			pos++
		case 'M':
			if err := need(2); err != nil {
				return nil, err
			}
			stack = append(stack, int(binary.LittleEndian.Uint16(b[pos:])))
			pos += 2
		case 'J':
			if err := need(4); err != nil {
				return nil, err
			}
			stack = append(stack, int(int32(binary.LittleEndian.Uint32(b[pos:]))))
			pos += 4
		case 0x8a: // LONG1
			n := int(b[pos])
			pos++
			var v int64
			for i := n - 1; i >= 0; i-- {
				v = v<<8 | int64(b[pos+i])
			}
			if n > 0 && n < 8 && b[pos+n-1]&0x80 != 0 {
				v -= 1 << (8 * n)
			}
			pos += n
			stack = append(stack, int(v))
		case 'G': // BINFLOAT
			stack = append(stack, math.Float64frombits(binary.BigEndian.Uint64(b[pos:])))
			pos += 8
		case 'X': // BINUNICODE
			n := u32()
			stack = append(stack, string(b[pos:pos+n]))
			pos += n
		case 0x8c: // SHORT_BINUNICODE
			n := int(b[pos])
			pos++
			stack = append(stack, string(b[pos:pos+n]))
			pos += n
		case 'U': // SHORT_BINSTRING
			n := int(b[pos])
			pos++
			stack = append(stack, string(b[pos:pos+n]))
			pos += n
		case 'T': // BINSTRING
			n := u32()
			stack = append(stack, string(b[pos:pos+n]))
			pos += n
		case 'C': // SHORT_BINBYTES
			n := int(b[pos])
			pos++
			stack = append(stack, b[pos:pos+n])
			pos += n
		case 'q': // BINPUT
			memo[int(b[pos])] = stack[len(stack)-1]
			pos++
		case 'r': // LONG_BINPUT
			memo[u32()] = stack[len(stack)-1]
		case 0x94: // MEMOIZE
			memo[len(memo)] = stack[len(stack)-1]
		case 'h': // BINGET
			stack = append(stack, memo[int(b[pos])])
			pos++
		case 'j': // LONG_BINGET
			stack = append(stack, memo[u32()])
		case 'c': // GLOBAL
			mod := line()
			stack = append(stack, pthGlobal{mod, line()})
		case 0x93: // STACK_GLOBAL
			name, _ := pop().(string)
			mod, _ := pop().(string)
			stack = append(stack, pthGlobal{mod, name})
		case 't':
			stack = append(stack, popMark())
		case 0x85:
			stack = append(stack, []any{pop()})
		case 0x86:
			y := pop()
			stack = append(stack, []any{pop(), y})
		case 0x87:
			z, y := pop(), pop()
			stack = append(stack, []any{pop(), y, z})
		case 'Q': // BINPERSID: ('storage', <storage class>, key, location, numel)
			pid, _ := pop().([]any)
			st := &pthStorage{}
			if len(pid) >= 3 {
				if g, ok := pid[1].(pthGlobal); ok {
					st.dtype = g.name
				}
				st.key, _ = pid[2].(string)
			}
			stack = append(stack, st)
		case 'R': // REDUCE
			args, _ := pop().([]any)
			fn := pop()
			if g, ok := fn.(pthGlobal); ok && g.name == "OrderedDict" {
				stack = append(stack, map[string]any{})
			} else {
				stack = append(stack, &pthReduce{fn, args})
			}
		case 0x81: // NEWOBJ
			args, _ := pop().([]any)
			stack = append(stack, &pthReduce{pop(), args})
		case 'b': // BUILD: state for the object below (ignored)
			pop()
		case 's': // SETITEM
			v := pop()
			k := pop()
			setItems(stack[len(stack)-1], []any{k, v})
		case 'u': // SETITEMS
			kv := popMark()
			setItems(stack[len(stack)-1], kv)
		case 'a': // APPEND
			v := pop()
			if l, ok := stack[len(stack)-1].([]any); ok {
				stack[len(stack)-1] = append(l, v)
			}
		case 'e': // APPENDS
			items := popMark()
			if l, ok := stack[len(stack)-1].([]any); ok {
				stack[len(stack)-1] = append(l, items...)
			}
		default:
			return nil, fmt.Errorf("pickle: opcode 0x%02x not supported", op)
		}
	}
	return nil, errors.New("pickle: no STOP")
}
