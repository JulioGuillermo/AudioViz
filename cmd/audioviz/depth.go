package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"audioviz/internal/depth"
)

// runDepth: audioviz depth [-source ai|brightness] [-model name] [-detail ...] [-o out.png] <image>:
// the depth map of an image (white = near), as the studio makes it
func runDepth(args []string) {
	fs := flag.NewFlagSet("depth", flag.ExitOnError)
	src := fs.String("source", depth.SourceAI, "ai or brightness")
	model := fs.String("model", "small", "ai model: "+strings.Join(names(), ", "))
	detail := fs.String("detail", "high", "ai detail: normal (504 px), high (1008), max (1512), native (the image's size, by tiles)")
	out := fs.String("o", "depth.png", "output PNG (16-bit grey, white = near)")
	all := fs.Bool("all", false, "a video: every frame (kept in a temporary cache; -o gets the first)")
	fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: audioviz depth [-source ai|brightness] [-model name] [-detail normal|high|max|native] [-o out.png] <image>")
		os.Exit(2)
	}
	dir, err := os.MkdirTemp("", "audioviz-depth-cache-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)
	t0 := time.Now()
	last := ""
	frames := 0
	m, err := depth.Make(context.Background(), dir, depth.Request{Image: fs.Arg(0), Source: *src, Model: *model, Detail: *detail, All: *all},
		func(st string, p float64) {
			switch st {
			case "count":
				frames = int(p)
			case "frame":
				return
			case "frames":
				if i := int(p*float64(frames) + 0.5); frames > 0 && (i == 1 || i%25 == 0) {
					fmt.Printf("frame %d/%d %.1fs\n", i, frames, time.Since(t0).Seconds())
				}
				return
			}
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
	if frames > 0 {
		fmt.Printf("   %d frames: %.3f s per frame\n", frames, time.Since(t0).Seconds()/float64(frames))
	}
}

// names: the depth models, sorted
func names() []string {
	var n []string
	for k := range depth.Models {
		n = append(n, k)
	}
	slices.Sort(n)
	return n
}
