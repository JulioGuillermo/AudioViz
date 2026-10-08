package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"audioviz/internal/depth"
)

// runDepth: audioviz depth [-source ai|brightness] [-model small|base] [-detail ...] [-o out.png] <image>:
// the depth map of an image (white = near), as the studio makes it
func runDepth(args []string) {
	fs := flag.NewFlagSet("depth", flag.ExitOnError)
	src := fs.String("source", depth.SourceAI, "ai or brightness")
	model := fs.String("model", "small", "ai model: small or base")
	detail := fs.String("detail", "high", "ai detail: normal (504 px), high (1008), max (1512)")
	out := fs.String("o", "depth.png", "output PNG (16-bit grey, white = near)")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: audioviz depth [-source ai|brightness] [-model small|base] [-detail normal|high|max] [-o out.png] <image>")
		os.Exit(2)
	}
	dir, err := os.MkdirTemp("", "audioviz-depth-cache-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)
	t0 := time.Now()
	last := ""
	m, err := depth.Make(context.Background(), dir, depth.Request{Image: fs.Arg(0), Source: *src, Model: *model, Detail: *detail},
		func(st string, p float64) {
			if st != last {
				fmt.Printf("%s %.1fs\n", st, time.Since(t0).Seconds())
				last = st
			}
		})
	if err != nil {
		fail(err)
	}
	if err := m.Save(*out); err != nil {
		fail(err)
	}
	fmt.Printf("✅ %dx%d depth map in %.2fs: %s\n", m.W, m.H, time.Since(t0).Seconds(), *out)
}
