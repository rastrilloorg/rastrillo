package iconsets

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestFontAwesomeGlyphsFitTheirViewBox: no part of a Font Awesome
// drawing lies outside its viewBox, which an inline <svg> clips. Font
// Awesome 7 draws some glyphs taller than the 512 grid (the sun's rays
// reach -32 and 544), so a glyph keeps the viewBox its drawing needs.
func TestFontAwesomeGlyphsFitTheirViewBox(t *testing.T) {
	const slack = 1.0 // a rounding of the published path data
	for slug, glyph := range sets["font-awesome"].glyphs {
		var box [4]float64
		for index, field := range strings.Fields(glyph.ViewBox) {
			box[index], _ = strconv.ParseFloat(field, 64)
		}
		left, top := box[0]-slack, box[1]-slack
		right, bottom := box[0]+box[2]+slack, box[1]+box[3]+slack
		for _, data := range pathData.FindAllStringSubmatch(glyph.Body, -1) {
			for _, point := range pathPoints(data[1]) {
				if point[0] < left || point[0] > right ||
					point[1] < top || point[1] > bottom {
					t.Errorf("%s: the drawing reaches (%.1f, %.1f), "+
						"outside its viewBox %q",
						slug, point[0], point[1], glyph.ViewBox)
					break
				}
			}
		}
	}
}

var (
	pathData   = regexp.MustCompile(`\sd="([^"]*)"`)
	pathTokens = regexp.MustCompile(
		`[MmLlHhVvCcSsQqTtAaZz]|-?(?:\d+\.?\d*|\.\d+)(?:e-?\d+)?`)
)

// pathPoints walks SVG path data and returns points on the drawing:
// every end point, and each Bézier curve sampled along its length. An
// arc gives its end point only, which for Font Awesome's circles is
// where they touch their box.
func pathPoints(path string) [][2]float64 {
	tokens := pathTokens.FindAllString(path, -1)
	var out [][2]float64
	var pen, start, lastControl [2]float64
	var command string
	number := func() float64 {
		value, _ := strconv.ParseFloat(tokens[0], 64)
		tokens = tokens[1:]
		return value
	}
	curve := func(points ...[2]float64) {
		all := append([][2]float64{pen}, points...)
		for step := 1; step <= 16; step++ {
			out = append(out, bezier(all, float64(step)/16))
		}
	}
	for len(tokens) > 0 {
		if strings.ContainsAny(tokens[0][:1], "MmLlHhVvCcSsQqTtAaZz") {
			command, tokens = tokens[0], tokens[1:]
		}
		relative := command == strings.ToLower(command)
		at := func() [2]float64 {
			across, down := number(), number()
			if relative {
				return [2]float64{pen[0] + across, pen[1] + down}
			}
			return [2]float64{across, down}
		}
		upper := strings.ToUpper(command)
		switch upper {
		case "Z":
			pen = start
			continue
		case "M", "L", "T":
			pen = at()
			if upper == "M" {
				start = pen
				command = map[bool]string{true: "l", false: "L"}[relative]
			}
		case "H", "V":
			axis := map[string]int{"H": 0, "V": 1}[upper]
			value := number()
			if relative {
				value += pen[axis]
			}
			pen[axis] = value
		case "C", "S":
			first := [2]float64{2*pen[0] - lastControl[0],
				2*pen[1] - lastControl[1]}
			if upper == "C" {
				first = at()
			}
			second, end := at(), at()
			curve(first, second, end)
			pen, lastControl = end, second
		case "Q":
			control, end := at(), at()
			curve(control, end)
			pen = end
		case "A":
			for range 5 {
				number()
			}
			pen = at()
		}
		if upper != "C" && upper != "S" {
			lastControl = pen
		}
		out = append(out, pen)
	}
	return out
}

// bezier is the point at fraction along the curve through points (de
// Casteljau's construction).
func bezier(points [][2]float64, fraction float64) [2]float64 {
	for len(points) > 1 {
		next := make([][2]float64, len(points)-1)
		for index := range next {
			next[index] = [2]float64{
				points[index][0] +
					fraction*(points[index+1][0]-points[index][0]),
				points[index][1] +
					fraction*(points[index+1][1]-points[index][1]),
			}
		}
		points = next
	}
	return [2]float64{math.Round(points[0][0]*10) / 10,
		math.Round(points[0][1]*10) / 10}
}
