package cli

import "zen-board/internal/assets"

// assets passes the verb's arguments through to the asset CLI unchanged, so the
// `zen-board assets ...` surface (index, process-bg, generate, ui) keeps its own
// flag parsing and output format exactly as it had before the verbs existed.
func (a *App) assets(_ *Options, argv []string) error {
	return assets.RunCLI(argv, a.Config.AssetsDir)
}
