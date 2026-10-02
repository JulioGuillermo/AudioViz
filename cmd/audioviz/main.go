package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"audioviz/internal/config"
	"audioviz/internal/audio"
	"audioviz/internal/engine"
	"audioviz/internal/lyricsgen"
	"audioviz/internal/gpu"
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
	fs.StringVar(&flagOutput, "o", "", "Sobrescribir ruta del video de salida")
	fs.StringVar(&flagOutput, "output", "", "Sobrescribir ruta del video de salida")
	fs.StringVar(&flagAudio, "a", "", "Sobrescribir ruta del archivo de audio")
	fs.StringVar(&flagAudio, "audio", "", "Sobrescribir ruta del archivo de audio")
	fs.StringVar(&flagBg, "b", "", "Sobrescribir ruta del fondo imagen/video")
	fs.StringVar(&flagBg, "bg", "", "Sobrescribir ruta del fondo imagen/video")
	fs.Float64Var(&flagPreview, "p", 0, "Duración en segundos para renderizar un preview corto (ej. -p 10)")
	fs.Float64Var(&flagPreview, "preview", 0, "Duración en segundos para renderizar un preview corto")
	fs.Float64Var(&flagFrameAt, "frame-at", -1, "Segundo exacto del cual extraer un cuadro estático (ej. --frame-at 25.5)")
	fs.StringVar(&flagFrameOut, "frame-out", "preview_frame.png", "Ruta del cuadro estático a exportar")
	fs.StringVar(&flagRenderer, "renderer", "", "Backend de render: auto, gpu, cpu")
	fs.Float64Var(&flagStart, "ss", 0, "Segundo inicial del preview")
	fs.IntVar(&flagGPU, "gpu-device", -2, "Índice de GPU Vulkan (ver --list-gpus)")
	fs.BoolVar(&flagListGPUs, "list-gpus", false, "Lista las GPUs Vulkan disponibles")
	fs.StringVar(&flagLyrics, "l", "", "Letra sincronizada (.srt / .lrc) a mostrar")
	fs.StringVar(&flagLyrics, "lyrics", "", "Letra sincronizada (.srt / .lrc) a mostrar")
	fs.StringVar(&flagInit, "init", "", "Crea un archivo de proyecto plantilla YAML listo para editar")
	fs.BoolVar(&flagHelp, "h", false, "Muestra la ayuda")
	fs.BoolVar(&flagHelp, "help", false, "Muestra la ayuda")

	fs.Usage = printHelp

	// Reordenar argumentos para permitir que el archivo de proyecto esté antes o después de las opciones
	rawArgs := os.Args[1:]
	var flagArgs []string
	var positionalArgs []string

	for i := 0; i < len(rawArgs); i++ {
		arg := rawArgs[i]
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			// Si el flag toma valor y no contiene '=', tomar el siguiente argumento si existe
			if !strings.Contains(arg, "=") && i+1 < len(rawArgs) && !strings.HasPrefix(rawArgs[i+1], "-") {
				// Comprobar si es un flag booleano
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
			fmt.Printf("❌ Error creando plantilla: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✅ Plantilla de proyecto creada en: %s\n", flagInit)
		os.Exit(0)
	}

	projectFile := "project.yaml"
	if len(positionalArgs) > 0 {
		projectFile = positionalArgs[0]
	}

	if _, err := os.Stat(projectFile); os.IsNotExist(err) {
		fmt.Printf("⚠️  No se encontró el archivo de proyecto '%s'\n", projectFile)
		fmt.Printf("💡 Crea uno nuevo con: audioviz --init %s\n\n", projectFile)
		printHelp()
		os.Exit(1)
	}

	fmt.Printf("🚀 AudioViz v%s - Cargando proyecto: %s\n", version, projectFile)
	cfg, err := config.LoadConfig(projectFile)
	if err != nil {
		fmt.Printf("❌ Error cargando configuración: %v\n", err)
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
		fmt.Printf("\n❌ Error durante el renderizado: %v\n", err)
		os.Exit(1)
	}
}

// runLyrics genera un .lrc con tiempo por palabra a partir del audio y la letra en texto
func runLyrics(args []string) {
	fs := flag.NewFlagSet("lyrics", flag.ExitOnError)
	out := fs.String("o", "letra.lrc", "Archivo .lrc de salida")
	lang := fs.String("lang", "en", "Idioma de la letra (en, es, fr...)")
	fast := fs.Bool("fast", false, "Modo rápido: no separar la voz (menos preciso)")
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
		fmt.Println("Uso: audioviz lyrics <audio> <letra.txt> [-o letra.lrc] [--lang en] [--fast]")
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
			eta = fmt.Sprintf(" · quedan %s", fmtDur(step.ETA))
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
	fmt.Printf("✅ %d líneas → %s\n", len(res.Lines), *out)
}

func fmtDur(s float64) string {
	if s < 0 {
		s = 0
	}
	m := int(s) / 60
	return fmt.Sprintf("%d:%02d", m, int(s)%60)
}

// runStudio arranca la interfaz gráfica (servidor web local embebido en el binario)
func runStudio(args []string) {
	fs := flag.NewFlagSet("studio", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8787", "Dirección de escucha (usa 0.0.0.0:8787 para abrirlo desde otro equipo de la red)")
	noOpen := fs.Bool("no-open", false, "No abrir el navegador automáticamente")
	// El proyecto puede ir antes o después de las opciones
	var project string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		project, args = args[0], args[1:]
	}
	fs.Parse(args)
	if project == "" && fs.NArg() > 0 {
		project = fs.Arg(0)
	}
	err := studio.Run(studio.Options{Addr: *addr, Project: project, Version: version}, func(url string) {
		fmt.Printf("🎛️  AudioViz Studio en %s  (Ctrl+C para salir)\n", url)
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
	fmt.Printf(`AudioViz - Generador de Videos Musicales Reactivos al Audio
Uso:
  audioviz [ruta_al_proyecto.yaml] [opciones]
  audioviz studio [proyecto] [--addr 127.0.0.1:8787] [--no-open]   # interfaz gráfica
  audioviz lyrics <audio> <letra.txt> [-o letra.lrc] [--lang en] [--fast]   # letra sincronizada

Ejemplos:
  audioviz project.yaml                       # Renderiza el video completo (GPU Vulkan si hay)
  audioviz project.yaml -p 10 --ss 60         # Preview de 10 s empezando en el segundo 60
  audioviz project.yaml --frame-at 35.0       # Exporta el fotograma exacto en el segundo 35
  audioviz project.yaml --renderer cpu        # Fuerza render por CPU
  audioviz --list-gpus                        # Lista GPUs Vulkan
  audioviz --init nuevo.yaml                  # Genera una plantilla con todos los efectos

Opciones:
  -p, --preview <segundos>      Renderiza solo los primeros N segundos
  --frame-at <segundo>          Exporta un fotograma individual en PNG
  --frame-out <ruta>            Nombre del fotograma extraído (default: preview_frame.png)
  --ss <segundo>                Inicio del preview
  --renderer <auto|gpu|cpu>     Backend de render (auto = GPU Vulkan con caída a CPU)
  --gpu-device <n>              GPU Vulkan a usar (--list-gpus)
  -o, --output <ruta>           Sobrescribe la ruta del video final en el YAML
  -a, --audio <ruta>            Sobrescribe la ruta del audio en el YAML
  -b, --bg <ruta>               Sobrescribe la ruta del fondo (imagen o GIF) en el YAML
  -l, --lyrics <ruta>           Muestra una letra sincronizada (.srt / .lrc) y la activa
  --init <archivo.yaml>         Crea una plantilla YAML completa con comentarios
  -h, --help                    Muestra este mensaje de ayuda
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
