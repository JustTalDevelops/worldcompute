package chunk

import (
	"bytes"
	"fmt"
	"github.com/justtaldevelops/worldcompute/cube"
)

// StateToRuntimeID must hold a function to convert a name and its state properties to a runtime ID.
var StateToRuntimeID func(name string, properties map[string]interface{}) (runtimeID uint32, found bool)

// NetworkDecode decodes the network serialised data passed into a Chunk if successful. If not, the chunk
// returned is nil and the error non-nil.
// The sub chunk count passed must be that found in the LevelChunk packet.
//noinspection GoUnusedExportedFunction
func NetworkDecode(air uint32, data []byte, count int, oldBiomes bool, r cube.Range) (*Chunk, error) {
	var (
		c   = New(air, r)
		buf = bytes.NewBuffer(data)
		err error
	)
	for i := 0; i < count; i++ {
		index := uint8(i)
		c.sub[index], err = DecodeSubChunk(buf, c, &index, NetworkEncoding)
		if err != nil {
			return nil, err
		}
	}
	if oldBiomes {
		// Read the old biomes.
		biomes := make([]byte, 256)
		if _, err := buf.Read(biomes[:]); err != nil {
			return nil, fmt.Errorf("error reading biomes: %w", err)
		}

		// Make our 2D biomes 3D.
		for x := 0; x < 16; x++ {
			for z := 0; z < 16; z++ {
				id := biomes[(x&15)|(z&15)<<4]
				for y := r.Min(); y <= r.Max(); y++ {
					c.SetBiome(uint8(x), int16(y), uint8(z), uint32(id))
				}
			}
		}
	} else {
		var last *PalettedStorage
		for i := 0; i < len(c.sub); i++ {
			b, err := decodePalettedStorage(buf, NetworkEncoding, BiomePaletteEncoding)
			if err != nil {
				return nil, err
			}
			// b == nil means this paletted storage had the flag pointing to the previous one. It basically means we should
			// inherit whatever palette we decoded last.
			if i == 0 && b == nil {
				// This should never happen and there is no way to handle this.
				return nil, fmt.Errorf("first biome storage pointed to previous one")
			}
			if b == nil {
				// This means this paletted storage had the flag pointing to the previous one. It basically means we should
				// inherit whatever palette we decoded last.
				b = last
			} else {
				last = b
			}
			c.biomes[i] = b
		}
	}
	return c, nil
}

// DiskDecode decodes the data from a SerialisedData object into a chunk and returns it. If the data was
// invalid, an error is returned.
func DiskDecode(data SerialisedData, r cube.Range) (*Chunk, error) {
	air, ok := StateToRuntimeID("minecraft:air", nil)
	if !ok {
		panic("cannot find air runtime ID")
	}

	c := New(air, r)

	err := decodeBiomes(bytes.NewBuffer(data.Biomes), c, DiskEncoding)
	if err != nil {
		return nil, err
	}
	for i, sub := range data.SubChunks {
		if len(sub) == 0 {
			// No data for this sub chunk.
			continue
		}
		index := uint8(i)
		if c.sub[index], err = DecodeSubChunk(bytes.NewBuffer(sub), c, &index, DiskEncoding); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// DecodeSubChunk decodes a SubChunk from a bytes.Buffer. The Encoding passed defines how the block storages of the
// SubChunk are decoded.
func DecodeSubChunk(buf *bytes.Buffer, c *Chunk, index *byte, e Encoding) (*SubChunk, error) {
	ver, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading version: %w", err)
	}
	sub := NewSubChunk(c.air)
	switch ver {
	default:
		return nil, fmt.Errorf("unknown sub chunk version %v: can't decode", ver)
	case 0:
		ids := buf.Next(4096)
		if len(ids) != 4096 {
			return nil, fmt.Errorf("length of ids is smaller than 4096")
		}

		metadata := buf.Next(2048)
		if len(metadata) != 2048 {
			return nil, fmt.Errorf("length of metadata is smaller than 2048")
		}

		storage := newPalettedStorage(make([]uint32, 512), newPalette(4, make([]uint32, 0, 4)))
		for x := uint16(0); x < 16; x++ {
			for z := uint16(0); z < 16; z++ {
				for y := uint16(0); y < 8; y++ {
					i := (x << 8) | (z << 4) | (y << 1)

					meta := metadata[i>>1]

					err = setBlockData(storage, ids[i], meta&0xf, byte(x), byte(y<<1), byte(z))
					if err != nil {
						return nil, err
					}
					err = setBlockData(storage, ids[i|1], (meta>>4)&0xf, byte(x), byte((y<<1)|1), byte(z))
					if err != nil {
						return nil, err
					}
				}
			}
		}

		sub.storages = append(sub.storages, storage)
	case 8, 9:
		// Version 8 allows up to 256 layers for one sub chunk.
		storageCount, err := buf.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("error reading storage count: %w", err)
		}
		if ver == 9 {
			uIndex, err := buf.ReadByte()
			if err != nil {
				return nil, fmt.Errorf("error reading subchunk index: %w", err)
			}
			// The index as written here isn't the actual index of the subchunk within the chunk. Rather, it is the Y
			// value of the subchunk. This means that we need to translate it to an index.
			*index = uint8(int8(uIndex) - int8(c.r[0]>>4))
		}
		sub.storages = make([]*PalettedStorage, storageCount)

		for i := byte(0); i < storageCount; i++ {
			sub.storages[i], err = decodePalettedStorage(buf, e, BlockPaletteEncoding)
			if err != nil {
				return nil, err
			}
		}
	}
	return sub, nil
}

// setBlockData sets block data in a block storage instance, with it's ID, meta, and block position.
// It returns an error, which should be nil if everything was successful.
func setBlockData(storage *PalettedStorage, blockId, meta, x, y, z byte) error {
	state, ok := conversion[oldBlock{id: blockId, metadata: meta}]
	if !ok {
		// Try cases where meta should be ignored.
		state, ok = conversion[oldBlock{id: blockId}]
		if !ok {
			return fmt.Errorf("can't find runtime ID for id and meta: %v, %v", blockId, meta)
		}
	}

	id, ok := StateToRuntimeID(state.name, state.properties)
	if !ok {
		return fmt.Errorf("can't find runtime ID for state: %v, %v", state.name, state.properties)
	}

	storage.Set(x, y, z, id)
	return nil
}

// decodeBiomes reads the paletted storages holding biomes from buf and stores it into the Chunk passed.
func decodeBiomes(buf *bytes.Buffer, c *Chunk, e Encoding) error {
	var last *PalettedStorage
	if buf.Len() != 0 {
		for i := 0; i < len(c.sub); i++ {
			b, err := decodePalettedStorage(buf, e, BiomePaletteEncoding)
			if err != nil {
				return err
			}
			// b == nil means this paletted storage had the flag pointing to the previous one. It basically means we should
			// inherit whatever palette we decoded last.
			if i == 0 && b == nil {
				// This should never happen and there is no way to handle this.
				return fmt.Errorf("first biome storage pointed to previous one")
			}
			if b == nil {
				// This means this paletted storage had the flag pointing to the previous one. It basically means we should
				// inherit whatever palette we decoded last.
				b = last
			} else {
				last = b
			}
			c.biomes[i] = b
		}
	}
	return nil
}

// decodePalettedStorage decodes a PalettedStorage from a bytes.Buffer. The Encoding passed is used to read either a
// network or disk block storage.
func decodePalettedStorage(buf *bytes.Buffer, e Encoding, pe paletteEncoding) (*PalettedStorage, error) {
	blockSize, err := buf.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("error reading block size: %w", err)
	}
	if e == NetworkEncoding && blockSize&1 != 1 {
		e = NetworkPersistentEncoding
	}

	blockSize >>= 1
	if blockSize == 0x7f {
		return nil, nil
	}

	size := paletteSize(blockSize)
	uint32Count := size.uint32s()

	uint32s := make([]uint32, uint32Count)
	byteCount := uint32Count * 4

	data := buf.Next(byteCount)
	if len(data) != byteCount {
		return nil, fmt.Errorf("cannot read paletted storage (size=%v) %T: not enough block data present: expected %v bytes, got %v", blockSize, pe, byteCount, len(data))
	}
	for i := 0; i < uint32Count; i++ {
		// Explicitly don't use the binary package to greatly improve performance of reading the uint32s.
		uint32s[i] = uint32(data[i*4]) | uint32(data[i*4+1])<<8 | uint32(data[i*4+2])<<16 | uint32(data[i*4+3])<<24
	}
	p, err := e.decodePalette(buf, paletteSize(blockSize), pe)
	return newPalettedStorage(uint32s, p), err
}
