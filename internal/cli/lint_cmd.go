package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zen-board/internal/assets"
	"zen-board/internal/diag"
	"zen-board/internal/script"
)

// catalogLookup adapts the asset catalog to the linter's AssetLookup interface.
// It resolves through assets.Resolve - the same rule the compiler uses - so a
// script cannot pass the gate and then fail to load an asset.
type catalogLookup struct {
	dir string
	idx *assets.AssetIndex
}

func (l catalogLookup) Lookup(id string) (found, fromIndex, isSVG bool) {
	res := assets.Resolve(l.dir, l.idx, id)
	return res.Exists, res.FromIndex, res.IsSVG
}

func (l catalogLookup) SupportsVariants(id string) bool {
	res := assets.Resolve(l.dir, l.idx, id)
	return res.IsSVG
}

func (l catalogLookup) HasCursorPreset(name string) bool {
	if name == "" {
		return false
	}
	cfg := filepath.Join(l.dir, "cursors", name, "config.json")
	st, err := os.Stat(cfg)
	return err == nil && !st.IsDir()
}

// LintScript runs the grammar and catalog rules over a loaded script. The catalog
// is read, never written, and no network is touched.
func LintScript(src *script.Source, assetsDir string, skipAssets bool) *diag.Report {
	idx, _ := assets.LoadIndex(assetsDir)
	return script.Lint(src, catalogLookup{dir: assetsDir, idx: idx}, script.LintOptions{SkipAssetResolution: skipAssets})
}

// DSLHelpTable renders the grammar table for `check -dsl`. It is generated from
// the same specs the parser and linter use, so the documentation cannot drift.
func DSLHelpTable() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "zen-board scripting grammar (%d tags)\n\n", len(script.All()))
	for _, spec := range script.All() {
		line := "  " + script.CanonicalForm(spec)
		if spec.Status != script.StatusStable {
			line += "  [" + spec.Status + "]"
		}
		sb.WriteString(pad(line))
		if spec.Notes != "" {
			fmt.Fprintf(&sb, "    %s\n", spec.Notes)
		}
		if ex := spec.TagExamples(); len(ex) > 0 {
			fmt.Fprintf(&sb, "    e.g.  %s\n", strings.Join(ex, "   "))
		}
		if line := paramsLine(spec); line != "" {
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "layout/camera presets: %s\n", strings.Join(script.PresetNames(), " "))
	fmt.Fprintf(&sb, "cursor presets:        %s (plus any directory under assets/cursors/)\n", strings.Join(script.CursorPresets(), " "))
	return sb.String()
}

// pad writes the spec line without a newline so the notes can follow it.
func pad(line string) string { return line + "\n" }

func paramsLine(spec script.ActionSpec) string {
	var keys []string
	for _, p := range spec.Keys {
		if len(p.Enum) > 0 {
			keys = append(keys, fmt.Sprintf("%s=%s", p.Name, strings.Join(p.Enum, "|")))
			continue
		}
		unit := ""
		if p.Unit != "" {
			unit = p.Unit
		}
		keys = append(keys, fmt.Sprintf("%s=<%s>", p.Name, orDefault(unit, "value")))
	}
	if len(keys) == 0 {
		return ""
	}
	return "    named:  " + strings.Join(keys, " ") + "\n"
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
