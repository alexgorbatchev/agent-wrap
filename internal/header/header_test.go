package header

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/alexgorbatchev/agent-wrap/internal/project"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/lucasb-eyer/go-colorful"
)

func TestRenderedBlockSeparation(t *testing.T) {
	for i := range 360 {
		c := projectContext(fmt.Sprintf("project-%d", i))
		for _, kind := range []string{"branch", "subtree", "worktree", "unknown default"} {
			next := c
			switch kind {
			case "branch":
				next.Branch = "feature"
			case "subtree":
				next.Subtree = "src"
			case "worktree":
				next.Worktree = true
			case "unknown default":
				next.DefaultBranch = ""
			}
			view := uv.NewScreenBuffer(80, Rows)
			Draw(view, New(next), "Codex")
			pin, _ := colorful.MakeColor(view.CellAt(0, 0).Style.Bg)
			active, _ := colorful.MakeColor(view.CellAt(5, 0).Style.Bg)
			distance := pin.DistanceCIEDE2000(active)
			if distance < .3 {
				t.Errorf("%s %s block distance %.3f; want >=0.3", c.Identity, kind, distance)
			}
		}
	}
}

func projectContext(id string) project.Context {
	return project.Context{Identity: id, Name: id, Dir: "/work/" + id, Root: "/work/" + id, Branch: "trunk", DefaultBranch: "trunk"}
}

func TestHistoryReturnsAndColors(t *testing.T) {
	a, b, c := projectContext("A"), projectContext("B"), projectContext("C")
	s := New(a)
	if len(s.Trail) != 1 {
		t.Fatal("initial project missing")
	}
	s = s.Move(b).Move(c)
	if len(s.Trail) != 3 || s.Trail[0].Identity != "A" || s.Trail[1].Identity != "B" {
		t.Fatalf("trail = %+v", s.Trail)
	}
	old := s
	s = s.Move(b)
	if len(s.Trail) != 2 || len(old.Trail) != 3 || old.Trail[2].Identity != "C" {
		t.Fatal("history does not rewind immutably")
	}
	s = s.Move(a)
	if len(s.Trail) != 1 {
		t.Fatal("return did not clear later blocks")
	}
	a.Branch = "feature"
	s = s.Move(a)
	if len(s.Trail) != 1 || background(a) == bright(a.Identity) {
		t.Fatal("branch must split without adding history")
	}
	if background(projectContext("A")) != bright("A") {
		t.Fatal("default branch must use project color")
	}
	for _, key := range []string{"A", "B", "C", "branch/trunk", "subtree/测试"} {
		col := bright(key)
		if col != bright(key) || col.A != 255 || max(col.R, col.G, col.B) != 255 || min(col.R, col.G, col.B) < 80 {
			t.Fatalf("not deterministic and bright: %+v", col)
		}
	}
}

func TestDrawHistoryAndClipping(t *testing.T) {
	a, b, c := projectContext("A"), projectContext("B"), projectContext("C")
	for _, width := range []int{0, 1, 2, 3, 4, 7, 9, 10, 60} {
		t.Run(strings.Repeat("x", width), func(t *testing.T) {
			view := uv.NewScreenBuffer(width, 3)
			state := New(a).Move(b).Move(c)
			Draw(view, state, "Codex")
			for y := 0; y < 3; y++ {
				for x := 0; x < width; x++ {
					want := bright(c.Identity)
					if width >= 7 && x < 6 {
						want = bright(a.Identity)
						if x >= 3 {
							want = bright(b.Identity)
						}
					}
					if width >= 4 && width < 7 && x < 3 {
						want = bright(a.Identity)
					}
					cell := view.CellAt(x, y)
					if cell == nil || cell.Style.Bg != color.Color(want) {
						t.Fatalf("(%d,%d) bg=%+v want=%+v", x, y, cell, want)
					}
				}
			}
		})
	}
	view := uv.NewScreenBuffer(80, 3)
	Draw(view, New(a), "Codex\x1b[31m\nINJECT")
	var line strings.Builder
	for x := 0; x < 80; x++ {
		line.WriteString(view.CellAt(x, 0).Content)
	}
	if !strings.Contains(line.String(), "Codex") || strings.Contains(line.String(), "\x1b") {
		t.Fatalf("unsafe line = %q", line.String())
	}
	a.Branch = "feature"
	Draw(view, New(a), "Codex")
	for y := 0; y < 3; y++ {
		for x := 0; x < 80; x++ {
			want := background(a)
			if x < 3 {
				want = bright(a.Identity)
			}
			if view.CellAt(x, y).Style.Bg != color.Color(want) {
				t.Fatal("branch pin is not 3×3")
			}
		}
	}
}
