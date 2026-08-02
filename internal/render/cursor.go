package render

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "image/png"
)

type AnimationKeyframe struct {
	T       float64 `json:"t"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Rotate  float64 `json:"rotate"`
	Scale   float64 `json:"scale"`
	Opacity float64 `json:"opacity"`
	Ease    string  `json:"ease"`
}

type AnimationConfig struct {
	DurationSec float64             `json:"duration_sec"`
	Loop        bool                `json:"loop"`
	Keyframes   []AnimationKeyframe `json:"keyframes"`
}

type CursorPreset struct {
	Name        string               `json:"name"`
	Version     int                  `json:"version"`
	Sprites     map[string]string    `json:"sprites"`
	Tip         map[string]TipOffset `json:"tip"`
	SpriteCount int                  `json:"sprite_count"`
	Animation   *AnimationConfig     `json:"animation,omitempty"`
	// resolved at load time
	dir      string
	imgCache map[string]image.Image // style → decoded image
	rotCache map[string]map[int]image.Image
	mu       sync.Mutex
}

type TipOffset struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type SpriteLayer struct {
	Sprite  image.Image
	X, Y    int
	Opacity float64
}

type CursorRegistry struct {
	mu       sync.RWMutex
	baseDir  string
	presets  map[string]*CursorPreset
	loadOnGC bool
}

func NewCursorRegistry(baseDir string) *CursorRegistry {
	return &CursorRegistry{
		baseDir:  baseDir,
		presets:  make(map[string]*CursorPreset),
		loadOnGC: true,
	}
}

func (r *CursorRegistry) Get(name string) *CursorPreset {
	r.mu.RLock()
	p, ok := r.presets[name]
	r.mu.RUnlock()
	if ok {
		return p
	}
	if !r.loadOnGC {
		return nil
	}
	p = r.loadPreset(name)
	if p == nil {
		return nil
	}
	r.mu.Lock()
	r.presets[name] = p
	r.mu.Unlock()
	return p
}

func (r *CursorRegistry) loadPreset(name string) *CursorPreset {
	presetDir := filepath.Join(r.baseDir, name)
	cfgPath := filepath.Join(presetDir, "config.json")
	f, err := os.Open(cfgPath)
	if err != nil {
		log.Printf("[Cursor] No config for %q at %s: %v", name, cfgPath, err)
		return nil
	}
	defer f.Close()

	var preset CursorPreset
	if err := json.NewDecoder(f).Decode(&preset); err != nil {
		log.Printf("[Cursor] Invalid config for %q: %v", name, err)
		return nil
	}

	if preset.SpriteCount < 1 {
		preset.SpriteCount = 1
	}
	if preset.Tip == nil {
		preset.Tip = map[string]TipOffset{"default": {X: 0, Y: 0}}
	}
	if _, ok := preset.Tip["default"]; !ok {
		preset.Tip["default"] = TipOffset{}
	}

	preset.dir = presetDir
	preset.imgCache = make(map[string]image.Image)
	preset.rotCache = make(map[string]map[int]image.Image)

	for styleKey, relPath := range preset.Sprites {
		if strings.Contains(relPath, "..") || strings.HasPrefix(relPath, "/") {
			log.Printf("[Cursor] %q: rejecting unsafe sprite path %q", name, relPath)
			delete(preset.Sprites, styleKey)
			continue
		}
		spritePath := filepath.Join(presetDir, relPath)
		img, err := decodeImage(spritePath)
		if err != nil {
			log.Printf("[Cursor] %q: missing sprite %q: %v", name, relPath, err)
			delete(preset.Sprites, styleKey)
			continue
		}
		scaled := scaleImage(img, 256, 256)
		preset.imgCache[styleKey] = scaled
		preset.buildRotCacheForStyle(styleKey, scaled)
	}

	if len(preset.imgCache) == 0 {
		log.Printf("[Cursor] %q: no sprites loaded, discarding preset", name)
		return nil
	}

	return &preset
}

func (p *CursorPreset) buildRotCacheForStyle(styleKey string, sprite image.Image) {
	cache := make(map[int]image.Image)
	for _, deg := range cacheBuckets {
		if deg == 0 {
			cache[deg] = sprite
		} else {
			cache[deg] = rotateSprite(sprite, float64(deg))
		}
	}
	p.rotCache[styleKey] = cache
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func getTip(p *CursorPreset, styleKey string) (int, int) {
	if t, ok := p.Tip[styleKey]; ok {
		return t.X, t.Y
	}
	if t, ok := p.Tip["default"]; ok {
		return t.X, t.Y
	}
	return 0, 0
}

var knownEasing = map[string]func(float64) float64{
	"linear":            func(t float64) float64 { return t },
	"ease-in":           func(t float64) float64 { return t * t },
	"ease-out":          func(t float64) float64 { return t * (2 - t) },
	"ease-in-out":       EaseInOut,
	"ease-out-cubic":    EaseOutCubic,
	"ease-in-out-cubic": EaseInOutCubic,
}

func resolveEase(name string) func(float64) float64 {
	if fn, ok := knownEasing[name]; ok {
		return fn
	}
	log.Printf("[Cursor] Unknown easing %q, falling back to linear", name)
	return knownEasing["linear"]
}

func interpolateKeyframes(keyframes []AnimationKeyframe, t float64) (x, y, rotate, scale, opacity float64) {
	if len(keyframes) == 0 {
		return 0, 0, 0, 1, 1
	}
	if t <= keyframes[0].T {
		k := keyframes[0]
		return k.X, k.Y, k.Rotate, safeScale(k.Scale), k.Opacity
	}
	if t >= keyframes[len(keyframes)-1].T {
		k := keyframes[len(keyframes)-1]
		return k.X, k.Y, k.Rotate, safeScale(k.Scale), k.Opacity
	}
	for i := 0; i < len(keyframes)-1; i++ {
		a, b := keyframes[i], keyframes[i+1]
		if t >= a.T && t <= b.T {
			seg := (t - a.T) / (b.T - a.T)
			easeFn := resolveEase(b.Ease)
			seg = easeFn(seg)
			x = a.X + (b.X-a.X)*seg
			y = a.Y + (b.Y-a.Y)*seg
			rotate = a.Rotate + (b.Rotate-a.Rotate)*seg
			scale = safeScale(a.Scale + (b.Scale-a.Scale)*seg)
			opacity = a.Opacity + (b.Opacity-a.Opacity)*seg
			return
		}
	}
	return 0, 0, 0, 1, 1
}

func safeScale(s float64) float64 {
	if s <= 0 {
		return 0.01
	}
	return s
}

func CursorInterpolate(preset *CursorPreset, frameNum, fps int, baseX, baseY, angle int, handStyle string) []SpriteLayer {
	styleKey := handStyle
	if _, ok := preset.imgCache[styleKey]; !ok {
		if _, ok := preset.imgCache["default"]; ok {
			styleKey = "default"
		} else {
			return nil
		}
	}

	tipX, tipY := getTip(preset, styleKey)
	base := preset.imgCache[styleKey]

	var animX, animY, animRotate, animScale, animOpacity float64 = 0, 0, 1, 1, 1

	if preset.Animation != nil && len(preset.Animation.Keyframes) > 0 {
		dur := preset.Animation.DurationSec
		if dur <= 0 {
			dur = 1.0
		}
		animT := float64(frameNum) / float64(fps) / dur
		if preset.Animation.Loop {
			animT = animT - math.Floor(animT)
		} else if animT > 1.0 {
			animT = 1.0
		}
		animX, animY, animRotate, animScale, animOpacity = interpolateKeyframes(preset.Animation.Keyframes, animT)
	}

	layers := make([]SpriteLayer, preset.SpriteCount)

	rotated := base
	totalAngle := float64(angle) + animRotate
	bucket := snapAngle(int(math.Round(totalAngle)))
	if cache, ok := preset.rotCache[styleKey]; ok {
		if r, ok2 := cache[bucket]; ok2 {
			rotated = r
		}
	}

	for i := 0; i < preset.SpriteCount; i++ {
		sprite := rotated
		drawX := baseX + int(animX)
		drawY := baseY + int(animY)

		if animScale != 1 {
			b := sprite.Bounds()
			sw := int(float64(b.Dx()) * animScale)
			sh := int(float64(b.Dy()) * animScale)
			if sw > 0 && sh > 0 {
				sprite = scaleImage(sprite, sw, sh)
			}
		}

		if bucket != 0 {
			cx := float64(sprite.Bounds().Dx()) / 2
			cy := float64(sprite.Bounds().Dy()) / 2
			rad := float64(bucket) * math.Pi / 180.0
			cos, sin := math.Cos(rad), math.Sin(rad)
			rx := float64(tipX) - cx
			ry := float64(tipY) - cy
			tipX = int(math.Round(cx + rx*cos - ry*sin))
			tipY = int(math.Round(cy + rx*sin + ry*cos))
		}

		layers[i] = SpriteLayer{
			Sprite:  sprite,
			X:       drawX - tipX,
			Y:       drawY - tipY,
			Opacity: animOpacity,
		}
	}

	return layers
}

func DrawCursorLayers(dst draw.Image, layers []SpriteLayer) {
	for _, l := range layers {
		if l.Sprite == nil {
			continue
		}
		if l.Opacity < 0.01 {
			continue
		}
		b := l.Sprite.Bounds()
		offset := image.Pt(l.X, l.Y)
		if l.Opacity >= 0.99 {
			draw.Draw(dst, b.Add(offset), l.Sprite, image.Point{}, draw.Over)
		} else {
			mask := image.NewAlpha(b)
			for py := 0; py < b.Dy(); py++ {
				for px := 0; px < b.Dx(); px++ {
					mask.SetAlpha(px, py, color.Alpha{A: alphaFromFloat(l.Opacity)})
				}
			}
			draw.DrawMask(dst, b.Add(offset), l.Sprite, image.Point{}, mask, image.Point{}, draw.Over)
		}
	}
}

func alphaFromFloat(f float64) uint8 {
	v := int(f * 255)
	if v > 255 {
		v = 255
	}
	if v < 0 {
		v = 0
	}
	return uint8(v)
}

func FallbackPlaceholderSprite() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	red := image.NewUniform(color.RGBA{255, 0, 0, 255})
	draw.Draw(img, img.Bounds(), red, image.Point{}, draw.Src)
	return img
}
