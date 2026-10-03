// Package header draws immutable project history into frame header cells.
package header

import (
	"crypto/sha256"
	"encoding/binary"
	"image/color"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/alexgorbatchev/agent-wrap/internal/project"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"
)

const Rows = 3
const pinWidth = 3

// State owns its history; Move never mutates previously submitted states.
type State struct {
	Current project.Context
	Trail   []project.Context
}

func New(initial project.Context) State {
	return State{Current: initial, Trail: []project.Context{initial}}
}

// Move rewinds history on return to a previously visited project.
func (s State) Move(next project.Context) State {
	i := slices.IndexFunc(s.Trail, func(p project.Context) bool { return p.Identity == next.Identity })
	trail := slices.Clone(s.Trail)
	if i >= 0 {
		trail = trail[:i+1]
		trail[i] = next
	} else {
		trail = append(trail, next)
	}
	return State{Current: next, Trail: trail}
}

func hue(key string) float64 {
	sum := sha256.Sum256([]byte(key))
	return float64(binary.BigEndian.Uint64(sum[:8])%36000) / 100
}
func hueColor(h float64) color.RGBA {
	r, g, b := colorful.Hsv(h, .65, 1).RGB255()
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

// bright uses a fixed saturated palette with a stable hue for each identity.
func bright(key string) color.RGBA { return hueColor(hue(key)) }

// background separates context colors from the project hue by at least 60°.
func background(c project.Context) color.RGBA {
	if !c.Split() {
		return bright(c.Identity)
	}
	key := c.Identity + "\x00" + c.Branch + "\x00" + c.Subtree
	if c.Worktree {
		key += "\x00worktree"
	}
	return hueColor(math.Mod(hue(c.Identity)+60+math.Mod(hue(key), 240), 360))
}

// Draw fills three rows, preserving complete history pins when space allows.
func Draw(view uv.Screen, s State, name string) {
	b := view.Bounds()
	if b.Empty() {
		return
	}
	bg := background(s.Current)
	pins := make([]color.RGBA, 0, len(s.Trail))
	for _, p := range s.Trail[:max(0, len(s.Trail)-1)] {
		pins = append(pins, bright(p.Identity))
	}
	if s.Current.Split() {
		pins = append(pins, bright(s.Current.Identity))
	}
	capacity := max(0, (b.Dx()-1)/pinWidth)
	if len(pins) > capacity {
		pins = pins[:min(1, capacity)]
	}
	start := b.Min.X + len(pins)*pinWidth
	branch := s.Current.Branch
	if branch == "" {
		branch = "no Git repository"
	} else if s.Current.DefaultBranch == "" {
		branch += " · default branch unknown"
	}
	if s.Current.Worktree {
		branch += " · worktree"
	}
	text := safeText(name) + "\n" + safeText(s.Current.Name+" · "+s.Current.Dir) + "\n" + safeText(branch)
	uv.NewStyledString(text).Draw(view, uv.Rect(start+1, b.Min.Y, max(0, b.Max.X-start-1), min(Rows, b.Dy())))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := *view.CellAt(x, y)
			c.Style.Fg = color.Black
			c.Style.Bg = bg
			if x < start {
				c.Style.Bg = pins[(x-b.Min.X)/pinWidth]
			}
			view.SetCell(x, y, &c)
		}
	}
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}
