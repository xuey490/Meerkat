package captcha

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
)

// 验证码位图的尺寸常量：字形为 5x7 点阵，绘制时按 scale 放大。
const (
	// glyphCols 字形点阵列数。
	glyphCols = 5
	// glyphRows 字形点阵行数。
	glyphRows = 7
	// padding 画布四周留白（像素）。
	padding = 6
	// gap 相邻字符之间的水平间隔（像素）。
	gap = 6
)

// Render 把验证码文本绘制成 PNG，并编码为可直接用于 <img src> 的 Data URI。
//
// 图像干扰手段：每字符随机纵向偏移、逐行随机水平剪切、随机深色前景，
// 叠加随机噪点与干扰线，避免被简单的 OCR 直接识别。
//
// 参数 Parameters:
//   - text (string): 待绘制文本，建议仅含字符集内的字符；空串返回错误。
//   - width (int): 画布宽度（像素），<= 0 时按默认值 120。
//   - height (int): 画布高度（像素），<= 0 时按默认值 44。
//
// 返回 Returns:
//   - uri (string): 形如 "data:image/png;base64,..." 的图片地址。
//   - err (error): text 为空、含不支持字符或 PNG 编码失败时返回非 nil。
func Render(text string, width, height int) (uri string, err error) {
	if text == "" {
		return "", errors.New("captcha text is empty")
	}
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 44
	}
	scale := scaleFor(len(text), width, height)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	drawBackground(img)
	for index, char := range text {
		glyph, ok := glyphs[char]
		if !ok {
			return "", errors.New("captcha text contains unsupported character")
		}
		drawGlyph(img, glyph, index, scale, width, height)
	}
	drawNoise(img)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
}

// scaleFor 计算点阵放大倍数，使全部字符能落在画布内。
//
// 参数 Parameters:
//   - chars (int): 字符个数。
//   - width (int): 画布宽度（像素）。
//   - height (int): 画布高度（像素）。
//
// 返回 Returns:
//   - scale (int): 每个点阵像素对应的图片像素数，最小为 2。
func scaleFor(chars, width, height int) int {
	if chars <= 0 {
		return 2
	}
	byHeight := (height - 2*padding) / glyphRows
	usable := width - 2*padding - (chars-1)*gap
	byWidth := usable / (chars * glyphCols)
	scale := byHeight
	if byWidth < scale {
		scale = byWidth
	}
	if scale < 2 {
		scale = 2
	}
	return scale
}

// drawBackground 用浅色渐变填充画布，作为验证码底图，保证深色前景始终可读。
//
// 参数 Parameters:
//   - img (*image.RGBA): 目标画布，会被原地修改。
func drawBackground(img *image.RGBA) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			shift := uint8(y * 40 / bounds.Max.Y)
			img.Set(x, y, color.RGBA{R: 236 + shift/6, G: 240 + shift/8, B: 248, A: 255})
		}
	}
}

// drawGlyph 在画布上绘制单个字符，并施加随机偏移与剪切干扰。
//
// 参数 Parameters:
//   - img (*image.RGBA): 目标画布，会被原地修改。
//   - glyph ([]string): 该字符的 5x7 点阵，'#' 表示前景点。
//   - index (int): 字符序号（从 0 开始），决定横向起始位置。
//   - scale (int): 点阵放大倍数。
//   - width (int): 画布宽度（像素）。
//   - height (int): 画布高度（像素）。
func drawGlyph(img *image.RGBA, glyph []string, index, scale, width, height int) {
	glyphWidth := glyphCols * scale
	originX := padding + index*(glyphWidth+gap)
	offsetY := padding + randInt(height-2*padding-glyphRows*scale+1)
	shear := randInt(3) - 1 // 逐行水平剪切量，取值 -1/0/1，制造倾斜
	foreground := foregroundColor()
	for row, line := range glyph {
		for col, bit := range line {
			if bit != '#' {
				continue
			}
			baseX := originX + col*scale + shear*row
			baseY := offsetY + row*scale
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					x, y := baseX+dx, baseY+dy
					if x < 0 || x >= width || y < 0 || y >= height {
						continue
					}
					img.Set(x, y, foreground)
				}
			}
		}
	}
}

// foregroundColor 随机返回一种深色前景，保证与浅色底图有足够对比度。
//
// 返回 Returns:
//   - c (color.RGBA): 深色前景色，不透明。
func foregroundColor() color.RGBA {
	palette := []color.RGBA{
		{R: 23, G: 74, B: 138, A: 255},
		{R: 30, G: 58, B: 138, A: 255},
		{R: 55, G: 65, B: 81, A: 255},
		{R: 120, G: 53, B: 15, A: 255},
	}
	return palette[randInt(len(palette))]
}

// drawNoise 在画布上叠加干扰线与噪点，增加机器识别难度。
//
// 参数 Parameters:
//   - img (*image.RGBA): 目标画布，会被原地修改。
func drawNoise(img *image.RGBA) {
	bounds := img.Bounds()
	width, height := bounds.Max.X, bounds.Max.Y
	noise := color.RGBA{R: 120, G: 130, B: 150, A: 255}
	for i := 0; i < 4; i++ {
		x := randInt(width)
		y := randInt(height)
		length := width/4 + randInt(width/3)
		step := 1
		if randInt(2) == 0 {
			step = -1
		}
		for stepIndex := 0; stepIndex < length; stepIndex++ {
			px, py := x+stepIndex, y+step*stepIndex/2
			if px < 0 || px >= width || py < 0 || py >= height {
				break
			}
			img.Set(px, py, noise)
		}
	}
	for i := 0; i < width*height/40; i++ {
		img.Set(randInt(width), randInt(height), noise)
	}
}

// glyphs 是验证码字符的 5x7 点阵字面量，'#' 为前景点，'.' 为背景点。
var glyphs = map[rune][]string{
	'A': {".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'B': {"####.", "#...#", "#...#", "####.", "#...#", "#...#", "####."},
	'C': {".###.", "#...#", "#....", "#....", "#....", "#...#", ".###."},
	'D': {"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."},
	'E': {"#####", "#....", "#....", "####.", "#....", "#....", "#####"},
	'F': {"#####", "#....", "#....", "####.", "#....", "#....", "#...."},
	'G': {".###.", "#...#", "#....", "#.###", "#...#", "#...#", ".###."},
	'H': {"#...#", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'J': {"..###", "...#.", "...#.", "...#.", "...#.", "#..#.", ".##.."},
	'K': {"#...#", "#..#.", "#.#..", "##...", "#.#..", "#..#.", "#...#"},
	'M': {"#...#", "##.##", "#.#.#", "#...#", "#...#", "#...#", "#...#"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"},
	'P': {"####.", "#...#", "#...#", "####.", "#....", "#....", "#...."},
	'Q': {".###.", "#...#", "#...#", "#...#", "#.#.#", "#..#.", ".##.#"},
	'R': {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'S': {".####", "#....", "#....", ".###.", "....#", "....#", "####."},
	'T': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'V': {"#...#", "#...#", "#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W': {"#...#", "#...#", "#...#", "#.#.#", "#.#.#", "##.##", "#...#"},
	'X': {"#...#", "#...#", ".#.#.", "..#..", ".#.#.", "#...#", "#...#"},
	'Y': {"#...#", "#...#", ".#.#.", "..#..", "..#..", "..#..", "..#.."},
	'Z': {"#####", "....#", "...#.", "..#..", ".#...", "#....", "#####"},
	'2': {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	'3': {"####.", "....#", "....#", ".###.", "....#", "....#", "####."},
	'4': {"#..#.", "#..#.", "#..#.", "#####", "...#.", "...#.", "...#."},
	'5': {"#####", "#....", "#....", "####.", "....#", "....#", "####."},
	'6': {".###.", "#...#", "#....", "####.", "#...#", "#...#", ".###."},
	'7': {"#####", "....#", "...#.", "..#..", "..#..", "..#..", "..#.."},
	'8': {".###.", "#...#", "#...#", ".###.", "#...#", "#...#", ".###."},
	'9': {".###.", "#...#", "#...#", ".####", "....#", "#...#", ".###."},
}
