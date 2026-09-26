package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"audioviz/internal/config"
	"audioviz/internal/engine"
	"gopkg.in/yaml.v3"
)

const version = "1.0.0"

func main() {
	var (
		flagOutput   string
		flagAudio    string
		flagBg       string
		flagPreview  float64
		flagFrameAt  float64
		flagFrameOut string
		flagInit     string
		flagCodec    string
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
	fs.StringVar(&flagCodec, "codec", "", "Códec de video ('auto', 'h264_vaapi', 'h264_nvenc', 'libx264')")
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
				if arg != "-h" && arg != "--help" {
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

	if flagOutput != "" {
		cfg.Output.Path = flagOutput
	}
	if flagAudio != "" {
		cfg.Audio.Path = flagAudio
	}
	if flagBg != "" {
		cfg.Background.Path = flagBg
	}
	if flagCodec != "" {
		cfg.Output.Codec = flagCodec
	}

	renderOpts := engine.RenderOptions{
		MaxDurationSec: flagPreview,
		SingleFrameAt:  flagFrameAt,
		SingleFrameOut: flagFrameOut,
	}

	eng := engine.NewRenderEngine(cfg)
	if err := eng.Execute(renderOpts); err != nil {
		fmt.Printf("\n❌ Error durante el renderizado: %v\n", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`AudioViz - Generador de Videos Musicales Reactivos al Audio
Uso:
  audioviz [ruta_al_proyecto.yaml] [opciones]

Ejemplos:
  audioviz project.yaml                       # Renderiza el video completo usando GPU/CPU
  audioviz project.yaml -p 10                 # Renderiza una prueba rápida de los primeros 10 segundos
  audioviz project.yaml --frame-at 35.0       # Exporta el fotograma exacto en el segundo 35
  audioviz project.yaml --codec h264_vaapi    # Fuerza aceleración por hardware GPU (VAAPI)
  audioviz --init nuevo.yaml                  # Genera una plantilla con todos los efectos

Opciones:
  -p, --preview <segundos>      Renderiza solo los primeros N segundos
  --frame-at <segundo>          Exporta un fotograma individual en PNG
  --frame-out <ruta>            Nombre del fotograma extraído (default: preview_frame.png)
  --codec <nombre>              Códec de video ('auto', 'h264_vaapi', 'h264_nvenc', 'libx264')
  -o, --output <ruta>           Sobrescribe la ruta del video final en el YAML
  -a, --audio <ruta>            Sobrescribe la ruta del audio en el YAML
  -b, --bg <ruta>               Sobrescribe la ruta del fondo (imagen o video) en el YAML
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
