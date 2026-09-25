package card

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	W = 1200
	H = 630
)

var (
	bg     = color.RGBA{0x0c, 0x0a, 0x09, 0xff}
	fg     = color.RGBA{0xfa, 0xfa, 0xf9, 0xff}
	muted  = color.RGBA{0xa8, 0xa2, 0x9e, 0xff}
	fire   = color.RGBA{0xf9, 0x73, 0x16, 0xff}
	agree  = color.RGBA{0x38, 0xbd, 0xf8, 0xff}
	oppose = color.RGBA{0xf4, 0x3f, 0x5e, 0xff}
	track  = color.RGBA{0x29, 0x25, 0x24, 0xff}
)

var (
	once          sync.Once
	bold, regular *opentype.Font
)

func face(f *opentype.Font, size float64) font.Face {
	fc, _ := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	return fc
}

func text(dst draw.Image, fc font.Face, c color.Color, x, y int, s string) int {
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: fc, Dot: fixed.P(x, y)}
	d.DrawString(s)
	return d.Dot.X.Round()
}

func textRight(dst draw.Image, fc font.Face, c color.Color, right, y int, s string) {
	text(dst, fc, c, right-font.MeasureString(fc, s).Round(), y, s)
}

func wrap(fc font.Face, s string, width, max int) []string {
	var lines []string
	cur := ""
	for _, w := range strings.Fields(s) {
		try := strings.TrimSpace(cur + " " + w)
		if font.MeasureString(fc, try).Round() > width && cur != "" {
			lines = append(lines, cur)
			cur = w
		} else {
			cur = try
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > max {
		lines = lines[:max]
		lines[max-1] = strings.TrimRight(lines[max-1], ".,") + "…"
	}
	return lines
}

func rect(img *image.RGBA, x0, y0, x1, y1 int, c color.Color) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), image.NewUniform(c), image.Point{}, draw.Src)
}

type Match struct {
	Prompt   string
	NameA    string
	NameB    string
	TierA    string
	TierB    string
	Status   string
	Winner   string
	Headline string
	CrowdA   int
	CrowdB   int
	ScoreA   int
}

func Render(w io.Writer, m Match) error {
	once.Do(func() {
		bold, _ = opentype.Parse(gobold.TTF)
		regular, _ = opentype.Parse(goregular.TTF)
	})
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)

	x := text(img, face(bold, 34), fire, 64, 82, "cooked")
	label := "LIVE DEBATE"
	if m.Status == "done" {
		label = "VERDICT"
	} else if m.Status == "waiting" {
		label = "OPEN CHALLENGE"
	}
	text(img, face(bold, 22), muted, x+20, 80, label)

	for size := 64.0; size >= 38; size -= 6 {
		fc := face(bold, size)
		lines := wrap(fc, "“"+m.Prompt+"”", W-128, 3)
		if len(lines)*int(size*1.15) <= 230 || size <= 38 {
			y := 170
			for _, l := range lines {
				text(img, fc, fg, 64, y, l)
				y += int(size * 1.15)
			}
			break
		}
	}

	nameFace := face(bold, 40)
	b := m.NameB
	if b == "" {
		b = "you?"
	}
	text(img, face(bold, 20), agree, 64, 440, "AGREE · "+strings.ToUpper(m.TierA))
	text(img, nameFace, fg, 64, 486, m.NameA)
	right := "DISAGREE"
	if m.TierB != "" {
		right = strings.ToUpper(m.TierB) + " · DISAGREE"
	}
	textRight(img, face(bold, 20), oppose, W-64, 440, right)
	textRight(img, nameFace, fg, W-64, 486, b)
	text(img, face(bold, 34), muted, W/2-22, 480, "vs")

	share := 50
	if m.Status == "done" && m.ScoreA > 0 {
		share = m.ScoreA
	}
	if total := m.CrowdA + m.CrowdB; total > 0 {
		share = 100 * m.CrowdA / total
	}
	rect(img, 64, 516, W-64, 536, track)
	split := 64 + (W-128)*share/100
	rect(img, 64, 516, split, 536, agree)
	rect(img, split, 516, W-64, 536, oppose)

	bottom := fmt.Sprintf("%d%% agree · %d%% disagree", share, 100-share)
	if m.Status == "done" && m.Headline != "" {
		bottom = m.Headline
	} else if m.Status == "waiting" {
		bottom = "Think you can argue the other side? Tap in."
	}
	text(img, face(bold, 32), fg, 64, 590, bottom)
	return png.Encode(w, img)
}
