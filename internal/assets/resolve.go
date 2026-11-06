package assets

import (
	"os"
	"path/filepath"
	"strings"
)

// Resolved describes where an asset id points on disk.
type Resolved struct {
	Path      string
	Exists    bool
	FromIndex bool
	// IsSVG matters because an SVG is rasterised at its layout size, and a missing
	// w,h makes the event invisible instead of falling back to the source size.
	IsSVG bool
}

// Resolve maps an asset id to a file under assetsDir. It is the single rule the
// compiler, the asset loader and the linter all use: an entry in index.json wins,
// otherwise the id is a PNG in the assets directory.
//
// The pipeline had this logic open-coded four times, and the copies disagreed:
// validateAssets probed ".png" while the SVG pass would happily report a miss for
// an indexed .svg, so a script could pass validation and then draw nothing.
func Resolve(assetsDir string, idx *AssetIndex, id string) Resolved {
	if id == "" {
		return Resolved{}
	}
	if idx != nil {
		for _, a := range idx.Assets {
			if a.ID != id {
				continue
			}
			path := filepath.Join(assetsDir, a.File)
			return Resolved{
				Path:      path,
				Exists:    fileExists(path),
				FromIndex: true,
				IsSVG:     strings.EqualFold(filepath.Ext(a.File), ".svg"),
			}
		}
	}
	path := filepath.Join(assetsDir, id+".png")
	return Resolved{Path: path, Exists: fileExists(path)}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
