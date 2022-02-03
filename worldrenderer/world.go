package worldrenderer

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/justtaldevelops/worldcompute/chunk"
	"github.com/justtaldevelops/worldcompute/mcdb"
	"github.com/justtaldevelops/worldcompute/world"
	"github.com/nfnt/resize"
	"image"
	"sync"
)

// LoadWorld loads a world and returns all of its chunks.
func LoadWorld(path string) (world.ChunkPos, map[world.ChunkPos]*chunk.Chunk) {
	prov, err := mcdb.New(path, world.Overworld)
	if err != nil {
		panic(err)
	}

	var s world.Settings
	prov.Settings(&s)

	centerPos := world.ChunkPos{int32(s.Spawn.X() >> 4), int32(s.Spawn.Z() >> 4)}
	chunks := make(map[world.ChunkPos]*chunk.Chunk)
	propagateChunk(prov, chunks, centerPos)

	err = prov.Close()
	if err != nil {
		panic(err)
	}

	return centerPos, chunks
}

// RenderWorld renders a world to *ebiten.Images.
func RenderWorld(scale int, chunkMu *sync.Mutex, chunks map[world.ChunkPos]*chunk.Chunk) map[world.ChunkPos]*ebiten.Image {
	chunkMu.Lock()
	defer chunkMu.Unlock()

	var renderMu sync.Mutex
	rendered := make(map[world.ChunkPos]*ebiten.Image)

	var wg sync.WaitGroup
	for pos, ch := range chunks {
		wg.Add(1)

		ch := ch
		pos := pos
		go func() {
			c := RenderChunk(scale, ch)
			renderMu.Lock()
			rendered[pos] = c
			renderMu.Unlock()
			wg.Done()
		}()
	}

	wg.Wait()
	return rendered
}

// RenderChunk renders a new chunk image from the given chunk.
func RenderChunk(scale int, ch *chunk.Chunk) *ebiten.Image {
	img := image.NewRGBA(image.Rectangle{Max: image.Point{X: 16, Y: 16}})
	for x := byte(0); x < 16; x++ {
		for z := byte(0); z < 16; z++ {
			y := ch.HighestBlock(x, z)
			name, properties, _ := chunk.RuntimeIDToState(ch.Block(x, y, z, 0))
			rid, ok := chunk.StateToRuntimeID(name, properties)
			if ok {
				material := materials[rid]
				img.Set(int(x), int(z), materialColours[material])
			}
		}
	}
	return ebiten.NewImageFromImage(resize.Resize(uint(scale*16), uint(scale*16), img, resize.NearestNeighbor))
}

// propagateChunk propagates a chunk in the chunks map, and then it's neighbours, until there are no chunks left.
func propagateChunk(prov *mcdb.Provider, chunks map[world.ChunkPos]*chunk.Chunk, pos world.ChunkPos) {
	if _, ok := chunks[pos]; ok {
		return
	}

	c, exists, err := prov.LoadChunk(pos)
	if err != nil {
		panic(err)
	}
	if !exists {
		return
	}

	chunks[pos] = c

	propagateChunk(prov, chunks, world.ChunkPos{pos.X(), pos.Z() + 1})
	propagateChunk(prov, chunks, world.ChunkPos{pos.X(), pos.Z() - 1})
	propagateChunk(prov, chunks, world.ChunkPos{pos.X() + 1, pos.Z()})
	propagateChunk(prov, chunks, world.ChunkPos{pos.X() - 1, pos.Z()})
}
