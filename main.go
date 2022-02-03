package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/justtaldevelops/worldcompute/chunk"
	"github.com/justtaldevelops/worldcompute/mcdb"
	"github.com/justtaldevelops/worldcompute/world"
	"github.com/justtaldevelops/worldcompute/worldrenderer"
	"github.com/pelletier/go-toml"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/auth"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/oauth2"
	"io/ioutil"
	"log"
	"os"
	"sync"
)

var (
	chunkMu  sync.Mutex
	chunks   = make(map[world.ChunkPos]*chunk.Chunk)
	renderer *worldrenderer.Renderer
)

// The following program implements a proxy that forwards players from one local address to a remote address.
func main() {
	config := readConfig()
	src := tokenSource()

	go func() {
		fmt.Println("worldcompute has loaded. connect to " + config.Connection.LocalAddress)
		p, err := minecraft.NewForeignStatusProvider(config.Connection.RemoteAddress)
		if err != nil {
			panic(err)
		}
		listener, err := minecraft.ListenConfig{
			StatusProvider: p,
		}.Listen("raknet", config.Connection.LocalAddress)
		if err != nil {
			panic(err)
		}
		defer listener.Close()
		for {
			c, err := listener.Accept()
			if err != nil {
				panic(err)
			}
			go handleConn(c.(*minecraft.Conn), listener, config, src)
		}
	}()

	renderer = worldrenderer.NewRendererDirect(8, 6.5, world.ChunkPos{}, &chunkMu, chunks)

	ebiten.SetWindowSize(1718, 1360)
	ebiten.SetWindowResizable(true)
	ebiten.SetWindowTitle("worldrenderer")
	if err := ebiten.RunGame(renderer); err != nil {
		log.Fatal(err)
	}
}

// handleConn handles a new incoming minecraft.Conn from the minecraft.Listener passed.
func handleConn(conn *minecraft.Conn, listener *minecraft.Listener, config config, src oauth2.TokenSource) {
	clientData := conn.ClientData()
	clientData.ServerAddress = config.Connection.RemoteAddress

	serverConn, err := minecraft.Dialer{
		TokenSource: src,
		ClientData:  clientData,
	}.Dial("raknet", config.Connection.RemoteAddress)
	if err != nil {
		fmt.Println(err)
		return
	}

	data := serverConn.GameData()
	data.GameRules = append(data.GameRules, []protocol.GameRule{{Name: "showCoordinates", Value: true}}...)
	fmt.Println(data.CustomBlocks)

	airRID, _ := chunk.StateToRuntimeID("minecraft:air", nil)
	oldFormat := data.BaseGameVersion == "1.17.40"
	worldRange := world.Overworld.Range()
	pos := data.PlayerPosition
	fmt.Println("Completed connection.")

	var g sync.WaitGroup
	g.Add(2)
	go func() {
		if err := conn.StartGame(data); err != nil {
			fmt.Println(err)
			return
		}
		g.Done()
	}()
	go func() {
		if err := serverConn.DoSpawn(); err != nil {
			fmt.Println(err)
			return
		}
		g.Done()
	}()
	g.Wait()
	fmt.Println("Spawned in.")

	go func() {
		defer listener.Disconnect(conn, "connection lost")
		defer serverConn.Close()
		for {
			pk, err := conn.ReadPacket()
			if err != nil {
				return
			}
			switch pk := pk.(type) {
			case *packet.SubChunkRequest:
				continue
			case *packet.MovePlayer:
				pos = pk.Position
				renderer.Recenter(world.ChunkPos{
					int32(pos.X()) >> 4,
					int32(pos.Z()) >> 4,
				})
			case *packet.Text:
				if pk.Message == "save" {
					fmt.Println("Saving chunks...")
					go func() {
						prov, err := mcdb.New("output", world.Overworld)
						if err != nil {
							panic(err)
						}
						for pos, c := range chunks {
							err = prov.SaveChunk(pos, c)
							if err != nil {
								panic(err)
							}
						}
						prov.SaveSettings(&world.Settings{
							Name:  data.WorldName,
							Spawn: [3]int{int(pos.X()), int(pos.Y()), int(pos.Z())},
							Time:  data.Time,
						})
						err = prov.Close()
						if err != nil {
							panic(err)
						}
						fmt.Println("Done.")
					}()
					continue
				}
			}
			if err := serverConn.WritePacket(pk); err != nil {
				if disconnect, ok := errors.Unwrap(err).(minecraft.DisconnectError); ok {
					_ = listener.Disconnect(conn, disconnect.Error())
				}
				return
			}
		}
	}()
	go func() {
		defer serverConn.Close()
		defer listener.Disconnect(conn, "connection lost")
		for {
			pk, err := serverConn.ReadPacket()
			if err != nil {
				if disconnect, ok := errors.Unwrap(err).(minecraft.DisconnectError); ok {
					_ = listener.Disconnect(conn, disconnect.Error())
				}
				return
			}
			switch pk := pk.(type) {
			case *packet.MovePlayer:
				if pk.EntityRuntimeID == data.EntityRuntimeID {
					pos = pk.Position
					renderer.Recenter(world.ChunkPos{
						int32(pos.X()) >> 4,
						int32(pos.Z()) >> 4,
					})
				}
			//case *packet.SubChunk:
			//	go func() {
			//		for _, entry := range pk.SubChunkEntries {
			//			if entry.Result == protocol.SubChunkResultSuccess {
			//				offsetPos := world.ChunkPos{
			//					pk.Position.X() + int32(entry.Offset[0]),
			//					pk.Position.Z() + int32(entry.Offset[2]),
			//				}
			//
			//				chunkMu.Lock()
			//				c, ok := chunks[offsetPos]
			//				if !ok {
			//					c = chunk.New(airRID, worldRange)
			//					chunks[offsetPos] = c
			//				}
			//				chunkMu.Unlock()
			//
			//				var ind byte
			//				newSub, err := chunk.DecodeSubChunk(bytes.NewBuffer(entry.RawPayload), c, &ind, chunk.NetworkEncoding)
			//				if err == nil {
			//					chunkMu.Lock()
			//					c.Sub()[ind] = newSub
			//					chunkMu.Unlock()
			//				} else {
			//					fmt.Println(err)
			//				}
			//			}
			//		}
			//	}()
			case *packet.ChangeDimension:
				chunkMu.Lock()
				for chunkPos := range chunks {
					delete(chunks, chunkPos)
				}
				chunkMu.Unlock()

				renderer.Rerender()
			case *packet.LevelChunk:
				go func() {
					c, err := chunk.NetworkDecode(airRID, pk.RawPayload, int(pk.SubChunkCount), oldFormat, worldRange)
					if err == nil {
						chunkMu.Lock()
						chunks[world.ChunkPos{pk.ChunkX, pk.ChunkZ}] = c
						chunkMu.Unlock()

						renderer.Rerender()
					} else {
						fmt.Println(err)
					}
				}()
				//switch pk.SubChunkRequestMode {
				//case protocol.SubChunkRequestModeLimited, protocol.SubChunkRequestModeLimitless:
				//	subChunkLimit := byte(24)
				//	if pk.SubChunkRequestMode == protocol.SubChunkRequestModeLimited {
				//		subChunkLimit = byte(pk.HighestSubChunk)
				//	}
				//
				//	var offsets [][3]byte
				//	for x := byte(0); x < 4; x++ {
				//		for z := byte(0); z < 4; z++ {
				//			for y := subChunkLimit; y > 0; y-- {
				//				offsets = append(offsets, [3]byte{x << 4, y, z << 4})
				//			}
				//		}
				//	}
				//	_ = serverConn.WritePacket(&packet.SubChunkRequest{
				//		Position: protocol.SubChunkPos{pk.Position.X(), 0, pk.Position.Z()},
				//		Offsets:  offsets,
				//	})
				//case protocol.SubChunkRequestModeLegacy:
				//	go func() {
				//		c, err := chunk.NetworkDecode(airRID, pk.RawPayload, int(pk.SubChunkCount), oldFormat, worldRange)
				//		if err == nil {
				//			chunkMu.Lock()
				//			chunks[world.ChunkPos{pk.Position.X(), pk.Position.Z()}] = c
				//			chunkMu.Unlock()
				//		} else {
				//			fmt.Println(err)
				//		}
				//	}()
				//}
			}
			if err := conn.WritePacket(pk); err != nil {
				return
			}
		}
	}()
}

type config struct {
	Connection struct {
		LocalAddress  string
		RemoteAddress string
	}
}

func readConfig() config {
	c := config{}
	if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
		f, err := os.Create("config.toml")
		if err != nil {
			log.Fatalf("error creating config: %v", err)
		}
		data, err := toml.Marshal(c)
		if err != nil {
			log.Fatalf("error encoding default config: %v", err)
		}
		if _, err := f.Write(data); err != nil {
			log.Fatalf("error writing encoded default config: %v", err)
		}
		_ = f.Close()
	}
	data, err := ioutil.ReadFile("config.toml")
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		log.Fatalf("error decoding config: %v", err)
	}
	if c.Connection.LocalAddress == "" {
		c.Connection.LocalAddress = "0.0.0.0:19132"
	}
	data, _ = toml.Marshal(c)
	if err := ioutil.WriteFile("config.toml", data, 0644); err != nil {
		log.Fatalf("error writing config file: %v", err)
	}
	return c
}

// tokenSource returns a token source for using with a gophertunnel client. It either reads it from the
// token.tok file if cached or requests logging in with a device code.
func tokenSource() oauth2.TokenSource {
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	token := new(oauth2.Token)
	tokenData, err := ioutil.ReadFile("token.tok")
	if err == nil {
		_ = json.Unmarshal(tokenData, token)
	} else {
		token, err = auth.RequestLiveToken()
		check(err)
	}
	src := auth.RefreshTokenSource(token)
	_, err = src.Token()
	if err != nil {
		// The cached refresh token expired and can no longer be used to obtain a new token. We require the
		// user to log in again and use that token instead.
		token, err = auth.RequestLiveToken()
		check(err)
		src = auth.RefreshTokenSource(token)
	}
	tok, _ := src.Token()
	b, _ := json.Marshal(tok)
	_ = ioutil.WriteFile("token.tok", b, 0644)
	return src
}
