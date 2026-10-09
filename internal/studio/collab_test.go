package studio

import (
	"reflect"
	"testing"
)

func TestTransformBatch(t *testing.T) {
	op := func(o, p string) jsonOp { return jsonOp{Op: o, Path: p} }
	cases := []struct {
		name         string
		ops, against []jsonOp
		want         []jsonOp
	}{
		{"past an insert before", []jsonOp{op("set", "/texts/1/color")}, []jsonOp{op("insert", "/texts/0")},
			[]jsonOp{op("set", "/texts/2/color")}},
		{"an insert after: no change", []jsonOp{op("set", "/texts/1/color")}, []jsonOp{op("insert", "/texts/3")},
			[]jsonOp{op("set", "/texts/1/color")}},
		{"its item removed", []jsonOp{op("set", "/texts/1/x"), op("set", "/texts/2/x")}, []jsonOp{op("remove", "/texts/1")},
			[]jsonOp{op("set", "/texts/1/x")}},
		{"another list", []jsonOp{op("set", "/layers/0/clips/1/x")}, []jsonOp{op("remove", "/layers/1/clips/0")},
			[]jsonOp{op("set", "/layers/0/clips/1/x")}},
		{"both insert at the same place: the first one stays", []jsonOp{op("insert", "/texts/1")}, []jsonOp{op("insert", "/texts/1")},
			[]jsonOp{op("insert", "/texts/2")}},
		{"its own insert moves what it removed", []jsonOp{op("insert", "/texts/0"), op("set", "/texts/3/x")}, []jsonOp{op("remove", "/texts/2")},
			[]jsonOp{op("insert", "/texts/0")}},
		{"an insert where an item was removed", []jsonOp{op("insert", "/texts/1")}, []jsonOp{op("remove", "/texts/1")},
			[]jsonOp{op("insert", "/texts/1")}},
	}
	for _, c := range cases {
		if got := transformBatch(c.ops, c.against); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestResolveOp(t *testing.T) {
	root := map[string]any{"texts": []any{map[string]any{}, map[string]any{}}}
	if o := resolveOp(root, jsonOp{Op: "insert", Path: "/texts/-"}); o.Path != "/texts/2" {
		t.Errorf("- → %s", o.Path)
	}
	if o := resolveOp(root, jsonOp{Op: "set", Path: "/texts/2"}); o.Op != "insert" {
		t.Errorf("set past the end → %s", o.Op)
	}
	if o := resolveOp(root, jsonOp{Op: "set", Path: "/texts/1/color"}); o.Op != "set" || o.Path != "/texts/1/color" {
		t.Errorf("set inside → %v", o)
	}
}
