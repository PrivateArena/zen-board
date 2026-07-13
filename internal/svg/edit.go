package svg

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/beevik/etree"
)

var rgbaRe = regexp.MustCompile(`rgba\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*,\s*[\d.]+\s*\)`)

type Variant map[string]string

func ModifySVG(rawXML []byte, variants Variant) ([]byte, error) {
	doc := etree.NewDocument()
	doc.ReadSettings = etree.ReadSettings{
		Entity: map[string]string{},
	}

	if err := doc.ReadFromBytes(rawXML); err != nil {
		return nil, fmt.Errorf("parsing xml document: %w", err)
	}

	ResolveCurrentColor(doc)

	for key, val := range variants {
		sanitizedVal := sanitizeValue(val)

		elements := doc.FindElements(fmt.Sprintf("//*[@id='%s']", key))
		for _, el := range elements {
			if el.SelectAttr("fill") != nil {
				el.CreateAttr("fill", sanitizedVal)
			}
			if el.SelectAttr("stroke") != nil {
				el.CreateAttr("stroke", sanitizedVal)
			}
			for _, child := range el.FindElements(".//") {
				if child.SelectAttr("fill") != nil {
					child.CreateAttr("fill", sanitizedVal)
				}
				if child.SelectAttr("stroke") != nil {
					child.CreateAttr("stroke", sanitizedVal)
				}
			}
		}
	}

	if styleEl := doc.FindElement("//style"); styleEl != nil {
		cssText := styleEl.Text()
		for key, val := range variants {
			sanitizedVal := sanitizeValue(val)
			cssText = replaceCSSClassFill(cssText, key, sanitizedVal)
		}
		styleEl.SetText(cssText)
	}

	doc.Indent(0)
	return doc.WriteToBytes()
}

// ResolveCurrentColor replaces `currentColor` with the effective color value
// per SVG spec: walks ancestor chain for a `color` attribute, defaults to #000000.
func ResolveCurrentColor(doc *etree.Document) {
	var walk func(el *etree.Element, inheritedColor string)
	walk = func(el *etree.Element, inheritedColor string) {
		effectiveColor := inheritedColor
		if el.SelectAttr("color") != nil {
			effectiveColor = el.SelectAttrValue("color", inheritedColor)
		}

		for _, attr := range []string{"fill", "stroke"} {
			if v := el.SelectAttrValue(attr, ""); v == "currentColor" {
				log.Printf("SVG compat: resolved currentColor -> %s for %s=%s", effectiveColor, attr, el.Tag)
				el.CreateAttr(attr, effectiveColor)
			}
		}

		for _, child := range el.ChildElements() {
			walk(child, effectiveColor)
		}
	}

	root := doc.Root()
	if root != nil {
		walk(root, "#000000")
	}
}

func resolveRGBA(raw []byte) []byte {
	matches := rgbaRe.FindAllSubmatch(raw, -1)
	for _, m := range matches {
		full := string(m[0])
		r, g, b := string(m[1]), string(m[2]), string(m[3])
		replacement := fmt.Sprintf("rgb(%s,%s,%s)", r, g, b)
		log.Printf("SVG compat: converted %s -> %s", full, replacement)
		raw = []byte(strings.Replace(string(raw), full, replacement, 1))
	}
	return raw
}

// PreprocessSVG applies compatibility fixes to SVG XML before rasterization:
//   - Resolves currentColor per SVG spec
//   - Converts rgba() to rgb() (oksvg doesn't support rgba)
func PreprocessSVG(rawXML []byte) ([]byte, error) {
	rawXML = resolveRGBA(rawXML)

	doc := etree.NewDocument()
	doc.ReadSettings = etree.ReadSettings{
		Entity: map[string]string{},
	}
	if err := doc.ReadFromBytes(rawXML); err != nil {
		return nil, fmt.Errorf("parsing xml document: %w", err)
	}

	ResolveCurrentColor(doc)

	doc.Indent(0)
	return doc.WriteToBytes()
}

func sanitizeValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "#") {
		hex := v[1:]
		if len(hex) == 3 || len(hex) == 6 {
			return v
		}
	}
	var out strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func replaceCSSClassFill(css, className, fillHex string) string {
	targetPattern := "." + className
	idx := strings.Index(css, targetPattern)
	if idx == -1 {
		return css
	}
	startBrace := strings.Index(css[idx:], "{")
	if startBrace == -1 {
		return css
	}
	endBrace := strings.Index(css[idx+startBrace:], "}")
	if endBrace == -1 {
		return css
	}

	body := css[idx+startBrace : idx+startBrace+endBrace]
	fillIdx := strings.Index(body, "fill:")
	if fillIdx == -1 {
		return css
	}
	semiIdx := strings.Index(body[fillIdx:], ";")
	if semiIdx == -1 {
		return css
	}

	oldFill := body[fillIdx : fillIdx+semiIdx]
	newFill := "fill: " + fillHex

	return strings.Replace(css, oldFill, newFill, 1)
}
