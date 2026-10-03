package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/engine"
	"audioviz/internal/gpu"
	"audioviz/internal/lyricsgen"
	"audioviz/internal/studio"
	"gopkg.in/yaml.v3"
)

const version = "2.0.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "studio" {
		runStudio(os.Args[2:])
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "lyrics" {
		runLyrics(os.Args[2:])
		return
	}
	var (
		flagOutput   string
		flagAudio    string
		flagBg       string
		flagPreview  float64
		flagFrameAt  float64
		flagFrameOut string
		flagInit     string
		flagRenderer string
		flagStart    float64
		flagGPU      int
		flagListGPUs bool
		flagLyrics   string
		flagHelp     bool
	)

	fs := flag.NewFlagSet("audioviz", flag.ContinueOnError)
	fs.StringVar(&flagOutput, "o", "", "Override output video path")
	fs.StringVar(&flagOutput, "output", "", "Override output video path")
	fs.StringVar(&flagAudio, "a", "", "Override audio file path")
	fs.StringVar(&flagAudio, "audio", "", "Override audio file path")
	fs.StringVar(&flagBg, "b", "", "Override background image/video path")
	fs.StringVar(&flagBg, "bg", "", "Override background image/video path")
	fs.Float64Var(&flagPreview, "p", 0, "Duration in seconds to render a short preview (e.g. -p 10)")
	fs.Float64Var(&flagPreview, "preview", 0, "Duration in seconds to render a short preview")
	fs.Float64Var(&flagFrameAt, "frame-at", -1, "Exact second to extract a still frame from (e.g. --frame-at 25.5)")
	fs.StringVar(&flagFrameOut, "frame-out", "preview_frame.png", "Path of the still frame to export")
	fs.StringVar(&flagRenderer, "renderer", "", "Render backend: auto, gpu, cpu")
	fs.Float64Var(&flagStart, "ss", 0, "Preview start second")
	fs.IntVar(&flagGPU, "gpu-device", -2, "Vulkan GPU index (see --list-gpus)")
	fs.BoolVar(&flagListGPUs, "list-gpus", false, "List available Vulkan GPUs")
	fs.StringVar(&flagLyrics, "l", "", "Synced lyrics (.srt / .lrc) to display")
	fs.StringVar(&flagLyrics, "lyrics", "", "Synced lyrics (.srt / .lrc) to display")
	fs.StringVar(&flagInit, "init", "", "Create a template YAML project file ready to edit")
	fs.BoolVar(&flagHelp, "h", false, "Show help")
	fs.BoolVar(&flagHelp, "help", false, "Show help")

	fs.Usage = printHelp

	// Reorder arguments so the project file may come before or after the options
	rawArgs := os.Args[1:]
	var flagArgs []string
	var positionalArgs []string

	for i := 0; i < len(rawArgs); i++ {
		arg := rawArgs[i]
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			// If the flag takes a value and has no '=', take the next argument if present
			if !strings.Contains(arg, "=") && i+1 < len(rawArgs) && !strings.HasPrefix(rawArgs[i+1], "-") {
				// Check whether it is a boolean flag
				if arg != "-h" && arg != "--help" && arg != "--list-gpus" && arg != "-list-gpus" {
					i++
					flagArgs = append(flagArgs, rawArgs[i])
				}
			}
		} else {
			positionalArgs = append(positionalArgs, arg)
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		os.Exit(1)
	}

	if flagListGPUs {
		fmt.Print(gpu.ListDevices())
		os.Exit(0)
	}

	if flagHelp {
		printHelp()
		os.Exit(0)
	}

	if flagInit != "" {
		if err := createTemplateProject(flagInit); err != nil {
			fmt.Printf("❌ Error creating template: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ Project template created at: %s\n", flagInit)
		os.Exit(0)
	}

	projectFile := "project.yaml"
	if len(positionalArgs) > 0 {
		projectFile = positionalArgs[0]
	}

	if _, err := os.Stat(projectFile); os.IsNotExist(err) {
		fmt.Printf("⚠️  Project file '%s' not found\n", projectFile)
		fmt.Printf("💡 Create a new one with: audioviz --init %s\n\n", projectFile)
		printHelp()
		os.Exit(1)
	}

	fmt.Printf("🚀 AudioViz v%s - Loading project: %s\n", version, projectFile)
	cfg, err := config.LoadConfig(projectFile)
	if err != nil {
		fmt.Printf("❌ Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	abs := func(p string) string { a, _ := filepath.Abs(p); return a }
	if flagOutput != "" {
		cfg.Output.Path = abs(flagOutput)
	}
	if flagAudio != "" {
		cfg.Audio.Path = abs(flagAudio)
	}
	if flagBg != "" {
		cfg.Timeline.Background[0].Cfg.Path = abs(flagBg)
	}
	if flagLyrics != "" {
		for i := range cfg.Timeline.Lyrics {
			cfg.Timeline.Lyrics[i].Cfg.Path = abs(flagLyrics)
			cfg.Timeline.Lyrics[i].Cfg.Enabled = true
		}
	}
	if flagRenderer != "" {
		cfg.Output.Renderer = flagRenderer
	}
	if flagGPU != -2 {
		cfg.Output.GPUDevice = flagGPU
	}

	renderOpts := engine.RenderOptions{
		MaxDurationSec: flagPreview,
		StartSec:       flagStart,
		SingleFrameAt:  flagFrameAt,
		SingleFrameOut: flagFrameOut,
	}

	if err := engine.Execute(cfg, renderOpts); err != nil {
		fmt.Printf("\n❌ Error during rendering: %v\n", err)
		os.Exit(1)
	}
}

// runLyrics generates a .lrc with per-word timing from the audio and the plain-text lyrics
func runLyrics(args []string) {
	fs := flag.NewFlagSet("lyrics", flag.ExitOnError)
	out := fs.String("o", "lyrics.lrc", "Output .lrc file")
	lang := fs.String("lang", "en", "Lyrics language (en, es, fr...)")
	fast := fs.Bool("fast", false, "Fast mode: skip vocal separation (less accurate)")
	var pos []string
	for len(args) > 0 {
		if strings.HasPrefix(args[0], "-") {
			fs.Parse(args)
			args = fs.Args()
			continue
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	if len(pos) != 2 {
		fmt.Println("Usage: audioviz lyrics <audio> <lyrics.txt> [-o lyrics.lrc] [--lang en] [--fast]")
		os.Exit(1)
	}
	txt, err := os.ReadFile(pos[1])
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	dec, err := audio.DecodeAudio(pos[0], 1)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	abs, _ := filepath.Abs(pos[0])
	st0, _ := os.Stat(abs)
	key := abs
	if st0 != nil {
		key = fmt.Sprintf("%s|%d|%d", abs, st0.Size(), st0.ModTime().Unix())
	}
	stereo := make([]float32, len(dec.PCM))
	sc := float32(1.0 / float64(int64(1)<<(dec.BitsPerSample-1)))
	for i, v := range dec.PCM {
		stereo[i] = float32(v) * sc
	}
	lastLine := ""
	res, err := lyricsgen.Generate(context.Background(), lyricsgen.Options{Stereo: stereo, SampleRate: dec.SampleRate, AudioKey: key,
		Lyrics: string(txt), Lang: *lang, Separate: !*fast}, func(s lyricsgen.Status) {
		step := s.Steps[s.Current]
		eta := ""
		if step.ETA > 0 {
			eta = fmt.Sprintf(" · %s left", fmtDur(step.ETA))
		}
		line := fmt.Sprintf("[%3.0f%%] %d/%d %s %3.0f%% · %s%s · %s", s.Progress*100, s.Current+1, len(s.Steps), step.Label, step.Progress*100, fmtDur(step.Elapsed), eta, step.Detail)
		if line != lastLine {
			fmt.Printf("\r\033[K%s", line)
			if step.State != "running" {
				fmt.Println()
			}
			lastLine = line
		}
	})
	fmt.Println()
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(res.LRC), 0644); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ %d lines → %s\n", len(res.Lines), *out)
}

func fmtDur(s float64) string {
	if s < 0 {
		s = 0
	}
	m := int(s) / 60
	return fmt.Sprintf("%d:%02d", m, int(s)%60)
}

// runStudio starts the GUI (local web server embedded in the binary)
func runStudio(args []string) {
	fs := flag.NewFlagSet("studio", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8787", "Listen address (use 0.0.0.0:8787 to open it from another machine on the network)")
	noOpen := fs.Bool("no-open", false, "Do not open the browser automatically")
	// The project may come before or after the options
	var project string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		project, args = args[0], args[1:]
	}
	fs.Parse(args)
	if project == "" && fs.NArg() > 0 {
		project = fs.Arg(0)
	}
	err := studio.Run(studio.Options{Addr: *addr, Project: project, Version: version}, func(url string) {
		fmt.Printf("🎛️  AudioViz Studio at %s  (Ctrl+C to quit)\n", url)
		if !*noOpen {
			for _, opener := range []string{"xdg-open", "open"} {
				if p, err := exec.LookPath(opener); err == nil {
					_ = exec.Command(p, url).Start()
					break
				}
			}
		}
	})
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`AudioViz - Audio-Reactive Music Video Generator
Usage:
  audioviz [path_to_project.yaml] [options]
  audioviz studio [project] [--addr 127.0.0.1:8787] [--no-open]   # GUI
  audioviz lyrics <audio> <lyrics.txt> [-o lyrics.lrc] [--lang en] [--fast]   # synced lyrics

Examples:
  audioviz project.yaml                       # Render the full video (Vulkan GPU if available)
  audioviz project.yaml -p 10 --ss 60         # 10 s preview starting at second 60
  audioviz project.yaml --frame-at 35.0       # Export the exact frame at second 35
  audioviz project.yaml --renderer cpu        # Force CPU rendering
  audioviz --list-gpus                        # List Vulkan GPUs
  audioviz --init new.yaml                    # Generate a template with all effects

Options:
  -p, --preview <seconds>       Render only the first N seconds
  --frame-at <second>           Export a single frame as PNG
  --frame-out <path>            Name of the extracted frame (default: preview_frame.png)
  --ss <second>                 Preview start
  --renderer <auto|gpu|cpu>     Render backend (auto = Vulkan GPU with CPU fallback)
  --gpu-device <n>              Vulkan GPU to use (--list-gpus)
  -o, --output <path>           Override the final video path from the YAML
  -a, --audio <path>            Override the audio path from the YAML
  -b, --bg <path>               Override the background path (image or GIF) from the YAML
  -l, --lyrics <path>           Show synced lyrics (.srt / .lrc) and enable them
  --init <file.yaml>            Create a YAML template with every option and its default
  -h, --help                    Show this help message
`)
}

func createTemplateProject(path string) error {
	cfg := config.DefaultConfig()
	cfg.Audio.Path = "audio.flac"
	cfg.Background.Path = "background.png"
	cfg.Output.Path = "output.mp4"

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	if dir := filepath.Dir(path); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	return os.WriteFile(path, data, 0644)
}
