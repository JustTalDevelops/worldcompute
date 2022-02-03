package worldrenderer

import (
	_ "embed"
	"github.com/justtaldevelops/worldcompute/chunk"
	_ "github.com/justtaldevelops/worldcompute/states"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

var (
	// materials is a map of block runtime ID to material.
	materials = make(map[uint32]byte)
	//go:embed material_mappings.nbt
	mappings []byte
)

// init initializes the stateToMaterial map.
func init() {
	var m [][]struct {
		Name       string                 `nbt:"name"`
		Properties map[string]interface{} `nbt:"properties"`
	}
	if err := nbt.Unmarshal(mappings, &m); err != nil {
		panic(err)
	}
	for id, states := range m {
		for _, s := range states {
			rid, ok := chunk.StateToRuntimeID(s.Name, s.Properties)
			if ok {
				materials[rid] = byte(id)
			}
		}
	}
}
