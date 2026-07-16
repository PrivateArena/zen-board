package model

func GetPresetLayout(preset string, canvasW, canvasH int) (x, y, w, h int) {
	halfW := canvasW / 2
	halfH := canvasH / 2
	switch preset {
	case "TL":
		return 0, 0, halfW, halfH
	case "TR":
		return halfW, 0, halfW, halfH
	case "BL":
		return 0, halfH, halfW, halfH
	case "BR":
		return halfW, halfH, halfW, halfH
	case "HT":
		return 0, 0, canvasW, halfH
	case "HB":
		return 0, halfH, canvasW, halfH
	case "LH":
		return 0, 0, halfW, canvasH
	case "RH":
		return halfW, 0, halfW, canvasH
	default:
		return 0, 0, canvasW, canvasH
	}
}
