package config

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// Lerp interpola dos bloques de config del mismo tipo: números y colores hex de forma continua,
// listas de igual longitud elemento a elemento, y el resto (textos, booleanos) cambia a mitad (k=0.5).
func Lerp[T any](a, b *T, k float64) T {
	if k <= 0 {
		return deepCopy(*a)
	}
	if k >= 1 {
		return deepCopy(*b)
	}
	out := deepCopy(*a)
	lerpValue(reflect.ValueOf(&out).Elem(), reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem(), k)
	return out
}

func lerpValue(dst, a, b reflect.Value, k float64) {
	switch a.Kind() {
	case reflect.Float64, reflect.Float32:
		dst.SetFloat(a.Float() + (b.Float()-a.Float())*k)
	case reflect.Int, reflect.Int32, reflect.Int64:
		dst.SetInt(int64(math.Round(float64(a.Int()) + float64(b.Int()-a.Int())*k)))
	case reflect.String:
		ca, okA := parseHex(a.String())
		cb, okB := parseHex(b.String())
		if okA && okB {
			var c [4]float64
			for i := range c {
				c[i] = ca[i] + (cb[i]-ca[i])*k
			}
			dst.SetString(formatHex(c, len(strings.TrimPrefix(a.String(), "#")) == 8 || len(strings.TrimPrefix(b.String(), "#")) == 8))
		} else if k >= 0.5 {
			dst.SetString(b.String())
		}
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if dst.Field(i).CanSet() {
				lerpValue(dst.Field(i), a.Field(i), b.Field(i), k)
			}
		}
	case reflect.Slice:
		if a.Len() == b.Len() {
			for i := 0; i < a.Len(); i++ {
				lerpValue(dst.Index(i), a.Index(i), b.Index(i), k)
			}
		} else if k >= 0.5 {
			dst.Set(reflect.ValueOf(deepCopyAny(b.Interface())).Convert(dst.Type()))
		}
	case reflect.Ptr:
		if !a.IsNil() && !b.IsNil() {
			nv := reflect.New(a.Elem().Type())
			lerpValue(nv.Elem(), a.Elem(), b.Elem(), k)
			dst.Set(nv)
		} else if k >= 0.5 {
			dst.Set(b)
		}
	default: // bool y demás: cambio a mitad
		if k >= 0.5 {
			dst.Set(b)
		}
	}
}

func deepCopyAny(v any) any {
	rv := reflect.ValueOf(v)
	cp := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
	reflect.Copy(cp, rv)
	return cp.Interface()
}

func parseHex(s string) ([4]float64, bool) {
	if !strings.HasPrefix(s, "#") {
		return [4]float64{}, false
	}
	h := s[1:]
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 && len(h) != 8 {
		return [4]float64{}, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return [4]float64{}, false
	}
	if len(h) == 6 {
		v = v<<8 | 0xFF
	}
	return [4]float64{float64(v >> 24 & 0xFF), float64(v >> 16 & 0xFF), float64(v >> 8 & 0xFF), float64(v & 0xFF)}, true
}

func formatHex(c [4]float64, alpha bool) string {
	b := func(x float64) int { return int(math.Max(0, math.Min(255, math.Round(x)))) }
	if alpha {
		return fmt.Sprintf("#%02x%02x%02x%02x", b(c[0]), b(c[1]), b(c[2]), b(c[3]))
	}
	return fmt.Sprintf("#%02x%02x%02x", b(c[0]), b(c[1]), b(c[2]))
}
