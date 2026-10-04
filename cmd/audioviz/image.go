package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/signal"
	"strings"

	"audioviz/internal/imagegen"
	"audioviz/internal/progress"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// audioviz image: generate an image from a prompt (and optional reference images)
func runImage(args []string) {
	fs := flag.NewFlagSet("image", flag.ExitOnError)
	prompt := fs.String("p", "", "Prompt: what the image shows")
	model := fs.String("model", "zimage", "Model: zimage (Z-Image Turbo), klein (FLUX.2 klein 4B), qwen (Qwen Image 2.1)")
	out := fs.String("o", "image.png", "Output .png file")
	w := fs.Int("W", 1024, "Width (rounded to a multiple of 32)")
	h := fs.Int("H", 576, "Height")
	steps := fs.Int("steps", 0, "Sampling steps (0 = the model's default)")
	seed := fs.Int64("seed", -1, "Seed (-1 = random)")
	list := fs.Bool("list", false, "List the models")
	var refs multiFlag
	fs.Var(&refs, "r", "Reference image (repeat for several; klein and qwen)")
	fs.Parse(args)
	if *list {
		for _, m := range imagegen.Models() {
			st := "downloaded"
			if n := m["missing"].(int64); n > 0 {
				st = fmt.Sprintf("%.1f GB to download", float64(n)/1e9)
			}
			fmt.Printf("%-7s %-16s refs %d · %s · %s\n        %s\n", m["id"], m["name"], m["refs"], st, m["license"], m["desc"])
		}
		return
	}
	if *prompt == "" && fs.NArg() > 0 {
		*prompt = strings.Join(fs.Args(), " ")
	}
	if *prompt == "" {
		fmt.Println(`Usage: audioviz image -p "prompt" [--model zimage|klein|qwen] [-r ref.png]... [-W 1024 -H 576] [--steps N] [--seed N] [-o image.png]`)
		fmt.Println("       audioviz image --list")
		os.Exit(1)
	}
	req := imagegen.Request{Model: *model, Prompt: *prompt, Width: *w, Height: *h, Steps: *steps, Seed: *seed}
	for _, p := range refs {
		img, err := loadImage(p)
		if err != nil {
			fail(err)
		}
		req.Refs = append(req.Refs, img)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	last := ""
	res, err := imagegen.Generate(ctx, req, func(s progress.Status) {
		st := s.Steps[s.Current]
		eta := ""
		if st.ETA > 0 {
			eta = fmt.Sprintf(" · %s left", fmtDur(st.ETA))
		}
		line := fmt.Sprintf("[%3.0f%%] %d/%d %s %3.0f%% · %s%s · %s", s.Progress*100, s.Current+1, len(s.Steps), st.Label, st.Progress*100, fmtDur(st.Elapsed), eta, st.Detail)
		if line != last {
			fmt.Printf("\r\033[K%s", line)
			if st.State != "running" {
				fmt.Println()
			}
			last = line
		}
	}, nil)
	fmt.Println()
	if err != nil {
		fail(err)
	}
	f, err := os.Create(*out)
	if err != nil {
		fail(err)
	}
	if err := png.Encode(f, res.Image); err != nil {
		fail(err)
	}
	if err := f.Close(); err != nil {
		fail(err)
	}
	fmt.Printf("✅ %s (seed %d, %s) → %s\n", res.Model, res.Seed, res.Device, *out)
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}

func fail(err error) {
	fmt.Printf("❌ %v\n", err)
	os.Exit(1)
}
