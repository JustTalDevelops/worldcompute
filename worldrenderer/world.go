package worldrenderer

import (
	"github.com/go-gl/mathgl/mgl64"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/justtaldevelops/worldcompute/dragonfly/chunk"
	"github.com/justtaldevelops/worldcompute/dragonfly/mcdb"
	"github.com/justtaldevelops/worldcompute/dragonfly/world"
	"github.com/nfnt/resize"
	"image"
	"sync"
)

// loadWorld loads a world and returns all of its chunks.
func loadWorld(path string) (mgl64.Vec2, map[world.ChunkPos]*chunk.Chunk) {
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

	return mgl64.Vec2{float64(s.Spawn.X()), float64(s.Spawn.Z())}, chunks
}

// renderWorld renders a world to *ebiten.Images.
func renderWorld(scale int, chunkMu *sync.Mutex, chunks map[world.ChunkPos]*chunk.Chunk) map[world.ChunkPos]*ebiten.Image {
	chunkMu.Lock()
	defer chunkMu.Unlock()

	var positions []world.ChunkPos
	for pos := range chunks {
		positions = append(positions, pos)
	}

	rendered := make(map[world.ChunkPos]*ebiten.Image)
	for _, pos := range positions {
		rendered[pos] = renderChunk(scale, pos, chunks)
	}
	return rendered
}

// renderChunk renders a new chunk image from the given chunk.
func renderChunk(scale int, pos world.ChunkPos, chunks map[world.ChunkPos]*chunk.Chunk) *ebiten.Image {
	img := image.NewRGBA(image.Rectangle{Max: image.Point{X: 16, Y: 16}})
	ch := chunks[pos]
	for x := byte(0); x < 16; x++ {
		for z := byte(0); z < 16; z++ {
			y := ch.HighestBlock(x, z)
			name, properties, _ := chunk.RuntimeIDToState(ch.Block(x, y, z, 0))
			rid, ok := chunk.StateToRuntimeID(name, properties)
			if ok {
				material := materials[rid]

				northTargetX, northTargetZ := x, z-1
				northWestTargetX, northWestTargetZ := x-1, z-1

				northChunk, northExists := chunks[world.ChunkPos{
					int32(northTargetX)>>4 + pos.X(),
					int32(northTargetZ)>>4 + pos.Z(),
				}]
				northWestChunk, northWestExists := chunks[world.ChunkPos{
					int32(northWestTargetX)>>4 + pos.X(),
					int32(northWestTargetZ)>>4 + pos.Z(),
				}]

				modifier := 0.8627
				if northExists && northWestExists {
					northY := northChunk.HighestBlock(northTargetX, northTargetZ)
					northWestY := northWestChunk.HighestBlock(northWestTargetX, northWestTargetZ)
					if northY > y && northWestY <= y {
						modifier = 0.7058
					} else if northY > y && northWestY > y {
						modifier = 0.5294
					} else if northY < y && northWestY < y {
						modifier = 1
					}
				}

				colour := materialColours[material]
				if material > 0 {
					colour.R = uint8(float64(colour.R) * modifier)
					colour.G = uint8(float64(colour.G) * modifier)
					colour.B = uint8(float64(colour.B) * modifier)
				}

				img.Set(int(x), int(z), colour)
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
