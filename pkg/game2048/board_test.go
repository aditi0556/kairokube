package game2048

import (
	"testing"
)

func TestSlideMergesEachTileOnce(t *testing.T) {
	b := NewBoard()
	b.Cells[0] = [Size]int{2, 2, 2, 2}
	changed, gained := b.slide(Left)
	if !changed {
		t.Fatal("expected board to change")
	}
	if b.Cells[0] != [Size]int{4, 4, 0, 0} {
		t.Fatalf("row after left slide = %v, want [4 4 0 0]", b.Cells[0])
	}
	if gained != 8 {
		t.Fatalf("gained = %d, want 8", gained)
	}
}

func TestSlideRightAndVertical(t *testing.T) {
	b := NewBoard()
	b.Cells[0][0] = 2
	b.Cells[1][0] = 2
	if changed, _ := b.slide(Down); !changed {
		t.Fatal("expected down slide to change board")
	}
	if b.Cells[3][0] != 4 {
		t.Fatalf("expected merged 4 at bottom-left, got %d", b.Cells[3][0])
	}

	r := NewBoard()
	r.Cells[2] = [Size]int{0, 4, 0, 4}
	r.slide(Right)
	if r.Cells[2] != [Size]int{0, 0, 0, 8} {
		t.Fatalf("row after right slide = %v, want [0 0 0 8]", r.Cells[2])
	}
}

func TestNewBoardStartsWithTwoDeterministicTiles(t *testing.T) {
	a, b := NewBoard(), NewBoard()
	if a.Cells != b.Cells {
		t.Fatal("NewBoard must be deterministic across nodes")
	}
	tiles := 0
	for r := 0; r < Size; r++ {
		for c := 0; c < Size; c++ {
			if a.Cells[r][c] != 0 {
				tiles++
			}
		}
	}
	if tiles != 2 {
		t.Fatalf("expected 2 starting tiles, got %d", tiles)
	}
}

func TestApplyNoChangeDoesNotSpawn(t *testing.T) {
	b := &Board{}
	b.Cells[0][0] = 2
	before := b.Cells
	changed, err := b.Apply(Left, RNGForSequence(1))
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if changed {
		t.Fatal("expected no change for an already-packed left move")
	}
	if b.Cells != before || b.MovesApplied != 0 {
		t.Fatal("no-op move must not spawn or count as a move")
	}
}

func TestApplyIsDeterministicForSameSequence(t *testing.T) {
	moves := []Direction{Left, Up, Right, Down, Left, Left, Up}
	run := func() *Board {
		b := NewBoard()
		for i, d := range moves {
			if _, err := b.Apply(d, RNGForSequence(int64(i+1))); err != nil {
				t.Fatalf("apply %d failed: %v", i, err)
			}
		}
		return b
	}
	a, b := run(), run()
	if a.Cells != b.Cells || a.Score != b.Score {
		t.Fatalf("replay diverged: %v vs %v", a.Cells, b.Cells)
	}
}

func TestIsOver(t *testing.T) {
	full := NewBoard()
	vals := [][]int{
		{2, 4, 2, 4},
		{4, 2, 4, 2},
		{2, 4, 2, 4},
		{4, 2, 4, 2},
	}
	for r := range vals {
		copy(full.Cells[r][:], vals[r])
	}
	if !full.IsOver() {
		t.Fatal("expected full board with no merges to be over")
	}
	full.Cells[0][0] = 4
	if full.IsOver() {
		t.Fatal("expected board with an available merge not to be over")
	}
}

func TestParseDirectionAndDecodeMove(t *testing.T) {
	if d, err := ParseDirection(" UP "); err != nil || d != Up {
		t.Fatalf("ParseDirection(' UP ') = %q, %v", d, err)
	}
	if _, err := ParseDirection("diagonal"); err == nil {
		t.Fatal("expected error for unknown direction")
	}
	if d, err := DecodeMove(`{"move":"left"}`); err != nil || d != Left {
		t.Fatalf("DecodeMove = %q, %v", d, err)
	}
	if _, err := DecodeMove(`not json`); err == nil {
		t.Fatal("expected error for malformed payload")
	}
}
