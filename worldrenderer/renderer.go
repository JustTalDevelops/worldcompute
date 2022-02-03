package worldrenderer

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/justtaldevelops/worldcompute/chunk"
	"github.com/justtaldevelops/worldcompute/world"
	"sync"
)

// Renderer implements the ebiten.Game interface.
type Renderer struct {
	scale int
	drift float64
	pos   mgl64.Vec2

	needsRerender bool
	shouldCenter  bool
	centerPos     world.ChunkPos

	chunkMu *sync.Mutex
	chunks  map[world.ChunkPos]*chunk.Chunk

	renderMu    *sync.Mutex
	renderCache map[world.ChunkPos]*ebiten.Image
}

// NewRenderer creates a new Renderer instance.
func NewRenderer(scale int, drift float64, path string) *Renderer {
	r := &Renderer{scale: scale, drift: drift, chunkMu: new(sync.Mutex), renderMu: new(sync.Mutex), shouldCenter: true}
	r.centerPos, r.chunks = loadWorld(path)
	r.renderCache = renderWorld(scale, r.chunkMu, r.chunks)
	return r
}

// NewRendererDirect creates a new renderer with the given chunks.
func NewRendererDirect(scale int, drift float64, centerPos world.ChunkPos, chunkMu *sync.Mutex, chunks map[world.ChunkPos]*chunk.Chunk) *Renderer {
	r := &Renderer{scale: scale, drift: drift, renderMu: new(sync.Mutex), shouldCenter: true}
	r.renderCache = renderWorld(scale, chunkMu, chunks)
	r.centerPos = centerPos
	r.chunkMu = chunkMu
	r.chunks = chunks
	return r
}

// Update proceeds the renderer state.
func (r *Renderer) Update() error {
	if ebiten.IsKeyPressed(ebiten.KeyUp) {
		r.pos = r.pos.Add(mgl64.Vec2{0, -r.drift})
	}
	if ebiten.IsKeyPressed(ebiten.KeyDown) {
		r.pos = r.pos.Add(mgl64.Vec2{0, r.drift})
	}
	if ebiten.IsKeyPressed(ebiten.KeyLeft) {
		r.pos = r.pos.Add(mgl64.Vec2{-r.drift, 0})
	}
	if ebiten.IsKeyPressed(ebiten.KeyRight) {
		r.pos = r.pos.Add(mgl64.Vec2{r.drift, 0})
	}

	oldScale := r.scale
	_, yOff := ebiten.Wheel()
	if yOff > 0 {
		r.scale++
	} else if yOff < 0 {
		r.scale--
	}
	if r.scale <= 0 {
		r.scale = 1
	}
	if oldScale != r.scale || len(r.renderCache) != len(r.chunks) {
		r.Rerender()
		r.pos = mgl64.Vec2{
			r.pos.X() / (float64(oldScale) * 16),
			r.pos.Y() / (float64(oldScale) * 16),
		}.Mul(float64(r.scale) * 16)
	}
	if r.needsRerender {
		r.renderCache = renderWorld(r.scale, r.chunkMu, r.chunks)
		r.needsRerender = false
	}
	return nil
}

// Draw draws the screen.
func (r *Renderer) Draw(screen *ebiten.Image) {
	screen.Fill(materialColours[0])

	w, h := screen.Size()
	chunkScale := float64(r.scale) * 16
	centerX, centerZ := float64(w/2), float64(h/2)
	if r.shouldCenter {
		r.pos = mgl64.Vec2{float64(r.centerPos.X()), float64(r.centerPos.Z())}.Mul(chunkScale)
		r.shouldCenter = false
	}

	r.renderMu.Lock()
	defer r.renderMu.Unlock()
	for pos, ch := range r.renderCache {
		chunkW, chunkH := ch.Bounds().Dx(), ch.Bounds().Dy()
		offsetX, offsetZ := float64(chunkW/2)+r.pos.X(), float64(chunkH/2)+r.pos.Y()

		chunkX, chunkZ := centerX+(float64(pos.X())*chunkScale), centerZ+(float64(pos.Z())*chunkScale)

		geo := ebiten.GeoM{}
		geo.Translate(chunkX-offsetX, chunkZ-offsetZ)
		screen.DrawImage(ch, &ebiten.DrawImageOptions{GeoM: geo})
	}
}

// Layout takes the outside size (e.g., the window size) and returns the (logical) screen size.
func (r *Renderer) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return outsideWidth, outsideHeight
}

// Rerender rerenders the world.
func (r *Renderer) Rerender() {
	r.needsRerender = true
}

// RerenderChunk rerenders the chunk at the given position.
func (r *Renderer) RerenderChunk(pos world.ChunkPos) {
	r.chunkMu.Lock()
	r.renderMu.Lock()
	defer r.chunkMu.Unlock()
	defer r.renderMu.Unlock()
	r.renderCache[pos] = renderChunk(r.scale, r.chunks[pos])
}

// Recenter centers the renderer on the given chunk.
func (r *Renderer) Recenter(pos world.ChunkPos) {
	r.centerPos = pos
	r.shouldCenter = true
}
