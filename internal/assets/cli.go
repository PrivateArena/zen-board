package assets

import (
	"flag"
	"fmt"
	"strings"
)

// RunCLI handles 'zen-board assets ...' subcommands.
func RunCLI(args []string, defaultAssetsDir string) error {
	if len(args) == 0 {
		PrintUsage()
		return fmt.Errorf("subcommand required")
	}

	subcmd := args[0]
	assetsDir := defaultAssetsDir
	if assetsDir == "" {
		assetsDir = "./assets"
	}

	switch subcmd {
	case "index":
		fs := flag.NewFlagSet("assets index", flag.ExitOnError)
		dirFlag := fs.String("dir", assetsDir, "Assets directory path")
		fs.Parse(args[1:])
		
		fmt.Printf("Indexing assets in %s...\n", *dirFlag)
		idx, err := AutoIndex(*dirFlag)
		if err != nil {
			return fmt.Errorf("indexing error: %w", err)
		}
		fmt.Printf("Successfully indexed. Total assets: %d\n", len(idx.Assets))
		return nil

	case "list":
		fs := flag.NewFlagSet("assets list", flag.ExitOnError)
		dirFlag := fs.String("dir", assetsDir, "Assets directory path")
		unprocessedOnly := fs.Bool("unprocessed", false, "Show only assets that need background removal")
		tagFilter := fs.String("tag", "", "Filter assets by tag")
		fs.Parse(args[1:])

		idx, err := LoadIndex(*dirFlag)
		if err != nil {
			return err
		}

		fmt.Printf("\n%-20s %-35s %-12s %-6s %-12s %s\n", "ID", "FILE", "STYLE", "BG", "SOURCE", "TAGS")
		fmt.Println(strings.Repeat("-", 100))

		count := 0
		for _, a := range idx.Assets {
			if *unprocessedOnly && !a.HasBg {
				continue
			}
			if *tagFilter != "" {
				hasTag := false
				for _, t := range a.Tags {
					if strings.EqualFold(t, *tagFilter) {
						hasTag = true
						break
					}
				}
				if !hasTag {
					continue
				}
			}
			bgStatus := "Clean"
			if a.HasBg {
				bgStatus = "Needs Bg"
			}
			fmt.Printf("%-20s %-35s %-12s %-6s %-12s %s\n", 
				a.ID, a.File, a.Style, bgStatus, a.Source, strings.Join(a.Tags, ","))
			count++
		}
		fmt.Printf("\nShowing %d assets.\n", count)
		return nil

	case "process-bg":
		fs := flag.NewFlagSet("assets process-bg", flag.ExitOnError)
		dirFlag := fs.String("dir", assetsDir, "Assets directory path")
		backend := fs.String("backend", "chromakey", "Background removal backend (chromakey, rembg or zen-lights)")
		lightsAddr := fs.String("lights", "http://localhost:8765", "zen-lights server address")
		fs.Parse(args[1:])

		return ProcessBackgrounds(*dirFlag, *backend, *lightsAddr)

	case "generate":
		fs := flag.NewFlagSet("assets generate", flag.ExitOnError)
		dirFlag := fs.String("dir", assetsDir, "Assets directory path")
		promptsFile := fs.String("prompts", "", "Path to prompts text file")
		style := fs.String("style", "vector", "Generation style: vector or realistic")
		lightsAddr := fs.String("lights", "http://localhost:8765", "zen-lights server address")
		fs.Parse(args[1:])

		if *promptsFile == "" {
			return fmt.Errorf("--prompts file is required")
		}
		return BatchGenerate(*dirFlag, *promptsFile, *style, *lightsAddr)

	case "ui":
		fs := flag.NewFlagSet("assets ui", flag.ExitOnError)
		dirFlag := fs.String("dir", assetsDir, "Assets directory path")
		port := fs.Int("port", 8080, "Port to serve Web GUI")
		lightsAddr := fs.String("lights", "http://localhost:8765", "zen-lights server address")
		fs.Parse(args[1:])

		return StartServer(*dirFlag, *port, *lightsAddr)

	default:
		PrintUsage()
		return fmt.Errorf("unknown assets subcommand: %s", subcmd)
	}
}

// PrintUsage prints the CLI usage details
func PrintUsage() {
	fmt.Println(`Usage: zen-board assets <subcommand> [options]

Subcommands:
  index       Walk the assets directory to auto-detect categories and IDs
  list        List all registered assets in the catalog
  process-bg  Clean up backgrounds for assets with has_bg=true
  generate    Batch generate assets via zen-lights from a prompts file
  ui          Start the local Web GUI management console

Run 'zen-board assets <subcommand> --help' for details on specific subcommand options.`)
}
