package world

import (
	_ "embed"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

var (
	//go:embed block_aliases.nbt
	blockAliasesData []byte
	// aliasMappings maps from a legacy block name alias to an updated name.
	aliasMappings = make(map[string]string)
)

// upgradeAliasEntry upgrades a possible alias block entry to the correct/updated block entry.
func upgradeAliasEntry(entry blockState) (blockState, bool) {
	if alias, ok := aliasMappings[entry.Name]; ok {
		entry.Name = alias
		return entry, true
	}
	if entry.Name == "minecraft:barrier" {
		entry.Name = "minecraft:info_update"
	}
	return blockState{}, false
}

// init creates conversions for each legacy and alias entry.
func init() {
	if err := nbt.Unmarshal(blockAliasesData, &aliasMappings); err != nil {
		panic(err)
	}
}
