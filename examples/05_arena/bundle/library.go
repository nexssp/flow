// Package arena ships the stateful arena actions for the Flow Arena example.
package arena

import (
	"context"
	"sync"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xerr"

	"github.com/nexssp/flow/core"
)

const (
	ID         = "arena"
	gridWidth  = 100
	gridHeight = 60
	sectorSize = 10
	sectorCols = gridWidth / sectorSize
	sectorRows = gridHeight / sectorSize
)

type Player struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
}

type SpawnReq struct {
	ID string `json:"id" validate:"required"`
}

type SpawnRes struct {
	OK bool `json:"ok"`
	X  int  `json:"x"`
	Y  int  `json:"y"`
}

type InputReq struct {
	ID string `json:"id" validate:"required"`
	DX int    `json:"dx"`
	DY int    `json:"dy"`
}

type InputRes struct {
	OK bool `json:"ok"`
}

type TickRes struct {
	Tick uint64 `json:"tick"`
}

type CountRes struct {
	Players int `json:"players"`
}

type ViewReq struct {
	ID     string `json:"id"     validate:"required"`
	Radius int    `json:"radius"`
}

type View struct {
	Tick   uint64   `json:"tick"`
	Self   Player   `json:"self"`
	Nearby []Player `json:"nearby"`
}

type character struct{ x, y, dx, dy int }

type world struct {
	mu      sync.RWMutex
	tick    uint64
	players map[string]character
	sectors [sectorRows][sectorCols]map[string]struct{}
}

var state = newWorld()

func newWorld() *world {
	w := &world{players: make(map[string]character, 4096)}
	for r := range sectorRows {
		for c := range sectorCols {
			w.sectors[r][c] = make(map[string]struct{}, 64)
		}
	}
	return w
}

func init() {
	core.Register(ID, Bundle)
}

func Bundle(_ map[string]string) core.Bundle {
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{{
			Name: ID,
			Actions: []action.AnyAction{
				action.New("arena.spawn", state.spawn).
					Description("Register a new player at the center of the grid").
					Build(),
				action.New("arena.input", state.input).
					Description("Set a player's velocity").
					Build(),
				action.New("arena.tick", state.advance).
					Description("Advance one tick; returns the new tick counter").
					Build(),
				action.New("arena.count", state.count).
					Description("Return the number of active players").
					Build(),
				action.New("arena.view", state.view).
					Description("Per-player AoI view; auto-spawns on first read").
					Build(),
			},
		}},
	}
}

func (w *world) spawn(_ context.Context, req SpawnReq) (SpawnRes, error) {
	if req.ID == "" {
		return SpawnRes{}, xerr.Validation("arena.spawn: id is required")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c, exists := w.players[req.ID]
	if exists {
		return SpawnRes{OK: true, X: c.x, Y: c.y}, nil
	}
	c = character{x: gridWidth / 2, y: gridHeight / 2}
	w.players[req.ID] = c
	row, col := sectorOf(c.x, c.y)
	w.sectors[row][col][req.ID] = struct{}{}
	return SpawnRes{OK: true, X: c.x, Y: c.y}, nil
}

func (w *world) input(_ context.Context, req InputReq) (InputRes, error) {
	if req.ID == "" {
		return InputRes{}, xerr.Validation("arena.input: id is required")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	c, exists := w.players[req.ID]
	if !exists {
		c = character{x: gridWidth / 2, y: gridHeight / 2}
		row, col := sectorOf(c.x, c.y)
		w.sectors[row][col][req.ID] = struct{}{}
	}
	c.dx, c.dy = req.DX, req.DY
	w.players[req.ID] = c
	return InputRes{OK: true}, nil
}

func (w *world) advance(_ context.Context, _ struct{}) (TickRes, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tick++
	for id, c := range w.players {
		oldRow, oldCol := sectorOf(c.x, c.y)
		c.x = clamp(c.x+c.dx, 0, gridWidth-1)
		c.y = clamp(c.y+c.dy, 0, gridHeight-1)
		newRow, newCol := sectorOf(c.x, c.y)
		if newRow != oldRow || newCol != oldCol {
			delete(w.sectors[oldRow][oldCol], id)
			w.sectors[newRow][newCol][id] = struct{}{}
		}
		w.players[id] = c
	}
	return TickRes{Tick: w.tick}, nil
}

func (w *world) count(_ context.Context, _ struct{}) (CountRes, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return CountRes{Players: len(w.players)}, nil
}

func (w *world) view(_ context.Context, req ViewReq) (View, error) {
	if req.ID == "" {
		return View{}, xerr.Validation("arena.view: id is required")
	}
	radius := req.Radius
	if radius <= 0 {
		radius = 1
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	self, exists := w.players[req.ID]
	if !exists {
		self = character{x: gridWidth / 2, y: gridHeight / 2}
		w.players[req.ID] = self
		row, col := sectorOf(self.x, self.y)
		w.sectors[row][col][req.ID] = struct{}{}
	}

	row, col := sectorOf(self.x, self.y)
	rowLo, rowHi := max(0, row-radius), min(sectorRows-1, row+radius)
	colLo, colHi := max(0, col-radius), min(sectorCols-1, col+radius)

	out := View{
		Tick:   w.tick,
		Self:   Player{ID: req.ID, X: self.x, Y: self.y},
		Nearby: make([]Player, 0, 16),
	}
	for r := rowLo; r <= rowHi; r++ {
		for c := colLo; c <= colHi; c++ {
			for id := range w.sectors[r][c] {
				if id == req.ID {
					continue
				}
				p := w.players[id]
				out.Nearby = append(out.Nearby, Player{ID: id, X: p.x, Y: p.y})
			}
		}
	}
	return out, nil
}

func sectorOf(x, y int) (row, col int) {
	return clamp(y/sectorSize, 0, sectorRows-1), clamp(x/sectorSize, 0, sectorCols-1)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
