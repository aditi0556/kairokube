// Package game2048 implements a deterministic 2048 board used as the stateful
// workload in the MS2M migration demo. Every tile spawn is driven by a random
// generator seeded from the message sequence number, so replaying the same
// moves on a migrated target reproduces the exact same board.
package game2048

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
)

// Size is the number of rows and columns on the board.
const Size = 4

// Direction is a slide direction accepted by the game.
type Direction string

const (
	Up    Direction = "up"
	Down  Direction = "down"
	Left  Direction = "left"
	Right Direction = "right"
)

// ErrInvalidDirection is returned for unknown move names.
var ErrInvalidDirection = errors.New("invalid direction")

// ParseDirection validates and normalizes a move name.
func ParseDirection(s string) (Direction, error) {
	d := Direction(strings.ToLower(strings.TrimSpace(s)))
	switch d {
	case Up, Down, Left, Right:
		return d, nil
	}
	return "", fmt.Errorf("%w: %q", ErrInvalidDirection, s)
}

// MoveCommand is the JSON payload carried inside a RabbitMQ application message.
type MoveCommand struct {
	Move string `json:"move"`
}

// DecodeMove extracts a validated direction from a message payload.
func DecodeMove(payload string) (Direction, error) {
	var cmd MoveCommand
	if err := json.Unmarshal([]byte(payload), &cmd); err != nil {
		return "", fmt.Errorf("invalid move payload: %w", err)
	}
	return ParseDirection(cmd.Move)
}

// Board is the observable game state. Cells holds tile values; 0 is empty.
type Board struct {
	Cells        [Size][Size]int `json:"cells"`
	Score        int64           `json:"score"`
	MovesApplied int64           `json:"moves_applied"`
}

// NewBoard returns a fresh board with two starting tiles. The starting tiles
// are spawned with a fixed seed so every node begins from the same state.
func NewBoard() *Board {
	b := &Board{}
	rng := rand.New(rand.NewPCG(0, 0x2048))
	b.spawn(rng)
	b.spawn(rng)
	return b
}

// RNGForSequence returns the deterministic generator used to spawn tiles for a
// message. Using the sequence number as the seed means a replayed message
// spawns the same tile in the same cell on any node.
func RNGForSequence(sequence int64) *rand.Rand {
	return rand.New(rand.NewPCG(uint64(sequence), 0x2048))
}

// coord maps position i along a line (0 = leading edge for the direction) to a cell.
func coord(d Direction, line, i int) (int, int) {
	switch d {
	case Left:
		return line, i
	case Right:
		return line, Size - 1 - i
	case Up:
		return i, line
	default: // Down
		return Size - 1 - i, line
	}
}

// slide moves and merges all tiles toward d. It returns whether any cell changed
// and the score gained from merges. Each tile merges at most once per move.
func (b *Board) slide(d Direction) (bool, int64) {
	changed := false
	var gained int64
	for line := 0; line < Size; line++ {
		vals := make([]int, 0, Size)
		for i := 0; i < Size; i++ {
			r, c := coord(d, line, i)
			if v := b.Cells[r][c]; v != 0 {
				vals = append(vals, v)
			}
		}
		merged := make([]int, 0, Size)
		for i := 0; i < len(vals); i++ {
			if i+1 < len(vals) && vals[i] == vals[i+1] {
				v := vals[i] * 2
				merged = append(merged, v)
				gained += int64(v)
				i++
			} else {
				merged = append(merged, vals[i])
			}
		}
		for i := 0; i < Size; i++ {
			r, c := coord(d, line, i)
			next := 0
			if i < len(merged) {
				next = merged[i]
			}
			if b.Cells[r][c] != next {
				changed = true
			}
			b.Cells[r][c] = next
		}
	}
	return changed, gained
}

// Apply slides the board in direction d. If the board changed, it spawns one
// tile using rng (2 with 90% probability, 4 otherwise). A move that changes
// nothing is a no-op and spawns nothing. It reports whether the board changed.
func (b *Board) Apply(d Direction, rng *rand.Rand) (bool, error) {
	if _, err := ParseDirection(string(d)); err != nil {
		return false, err
	}
	if rng == nil {
		return false, errors.New("random generator is required")
	}
	changed, gained := b.slide(d)
	if !changed {
		return false, nil
	}
	b.Score += gained
	b.MovesApplied++
	b.spawn(rng)
	return true, nil
}

func (b *Board) spawn(rng *rand.Rand) {
	type cell struct{ r, c int }
	var empty []cell
	for r := 0; r < Size; r++ {
		for c := 0; c < Size; c++ {
			if b.Cells[r][c] == 0 {
				empty = append(empty, cell{r, c})
			}
		}
	}
	if len(empty) == 0 {
		return
	}
	pick := empty[rng.IntN(len(empty))]
	value := 2
	if rng.IntN(10) == 0 {
		value = 4
	}
	b.Cells[pick.r][pick.c] = value
}

// IsOver reports whether no move can change the board.
func (b *Board) IsOver() bool {
	for r := 0; r < Size; r++ {
		for c := 0; c < Size; c++ {
			v := b.Cells[r][c]
			if v == 0 {
				return false
			}
			if c+1 < Size && b.Cells[r][c+1] == v {
				return false
			}
			if r+1 < Size && b.Cells[r+1][c] == v {
				return false
			}
		}
	}
	return true
}
