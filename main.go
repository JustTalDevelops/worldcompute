package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/justtaldevelops/worldcompute/dragonfly/chunk"
	"github.com/justtaldevelops/worldcompute/dragonfly/mcdb"
	"github.com/justtaldevelops/worldcompute/dragonfly/world"
	"github.com/justtaldevelops/worldcompute/worldrenderer"
	"github.com/pelletier/go-toml"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/auth"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/text"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"io/ioutil"
	"os"
	"sync"
)

var (
	chunkMu        sync.Mutex
	chunks         = make(map[world.ChunkPos]*chunk.Chunk)
	renderer       *worldrenderer.Renderer
	saveInProgress bool
)

// main starts the renderer and proxy.
func main() {
	log := logrus.New()
	log.Formatter = &logrus.TextFormatter{ForceColors: true}
	log.Level = logrus.DebugLevel

	src := tokenSource()
	conf, err := readConfig()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Println("worldcompute has loaded. connect to " + conf.Connection.LocalAddress)
		log.Println("redirecting connections to " + conf.Connection.RemoteAddress)

		p, err := minecraft.NewForeignStatusProvider(conf.Connection.RemoteAddress)
		if err != nil {
			panic(err)
		}
		listener, err := minecraft.ListenConfig{
			StatusProvider: p,
		}.Listen("raknet", conf.Connection.LocalAddress)
		if err != nil {
			panic(err)
		}
		defer listener.Close()

		for {
			c, err := listener.Accept()
			if err != nil {
				panic(err)
			}
			go handleConn(log, c.(*minecraft.Conn), listener, conf, src)
		}
	}()

	renderer = worldrenderer.NewRendererDirect(4, 6.5, mgl64.Vec2{}, &chunkMu, chunks)

	ebiten.SetWindowSize(1718, 1360)
	ebiten.SetWindowResizable(true)
	ebiten.SetWindowTitle("worldrenderer")
	if err := ebiten.RunGame(renderer); err != nil {
		log.Fatal(err)
	}
}

// handleConn handles a new incoming minecraft.Conn from the minecraft.Listener passed.
func handleConn(log *logrus.Logger, conn *minecraft.Conn, listener *minecraft.Listener, config config, src oauth2.TokenSource) {
	clientData := conn.ClientData()
	clientData.ServerAddress = config.Connection.RemoteAddress

	serverConn, err := minecraft.Dialer{
		TokenSource: src,
		ClientData:  clientData,
	}.Dial("raknet", config.Connection.RemoteAddress)
	if err != nil {
		log.Errorf("error connecting to %s: %v", config.Connection.RemoteAddress, err)
		return
	}

	data := serverConn.GameData()
	data.GameRules = append(data.GameRules, []protocol.GameRule{{Name: "showCoordinates", Value: true}}...)

	airRID, _ := chunk.StateToRuntimeID("minecraft:air", nil)
	oldFormat := data.BaseGameVersion == "1.17.40"
	if oldFormat {
		log.Debugf("old format detected, using old biomes")
	}
	worldRange := world.Overworld.Range()
	pos := data.PlayerPosition

	renderer.Recenter(mgl64.Vec2{
		float64(pos.X()),
		float64(pos.Z()),
	})

	log.Println("completed connection to " + config.Connection.RemoteAddress)

	var g sync.WaitGroup
	g.Add(2)
	go func() {
		if err := conn.StartGame(data); err != nil {
			log.Errorf("error starting game: %v", err)
			return
		}
		g.Done()
	}()
	go func() {
		if err := serverConn.DoSpawn(); err != nil {
			log.Errorf("error spawning: %v", err)
			return
		}
		g.Done()
	}()
	g.Wait()

	log.Printf("successfully spawned in to %s", config.Connection.RemoteAddress)

	// TODO: Clean up this shithole lmao
	go func() {
		defer listener.Disconnect(conn, "connection lost")
		defer serverConn.Close()
		for {
			pk, err := conn.ReadPacket()
			if err != nil {
				return
			}
			switch pk := pk.(type) {
			case *packet.PlayerAuthInput:
				pos = pk.Position
				renderer.Recenter(mgl64.Vec2{
					float64(pos.X()),
					float64(pos.Z()),
				})
			case *packet.MovePlayer:
				pos = pk.Position
				renderer.Recenter(mgl64.Vec2{
					float64(pos.X()),
					float64(pos.Z()),
				})
			case *packet.Text:
				if pk.Message == "reset" {
					chunkMu.Lock()
					for chunkPos := range chunks {
						delete(chunks, chunkPos)
					}
					chunkMu.Unlock()

					renderer.Rerender()
					continue
				}
				if saveInProgress {
					saveInProgress = false
					if pk.Message == "cancel" {
						_ = conn.WritePacket(&packet.Text{Message: text.Colourf("<red><bold><italic>Cancelled save.</italic></bold></red>")})
						continue
					}
					fileName := pk.Message
					_ = conn.WritePacket(&packet.Text{Message: text.Colourf("<aqua><bold><italic>Processing chunks to be saved...</italic></bold></aqua>")})
					go func() {
						prov, err := mcdb.New(fileName, world.Overworld)
						if err != nil {
							panic(err)
						}
						for pos, c := range chunks {
							c.Compact()
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

						_ = conn.WritePacket(&packet.Text{Message: text.Colourf("<green><bold><italic>Saved all chunks received to the \"%v\" folder!</italic></bold></green>", fileName)})
					}()
					continue
				}
				if pk.Message == "save" {
					_ = conn.WritePacket(&packet.Text{Message: text.Colourf("<yellow><bold><italic>What would you like to save the file as?</italic></bold></yellow>")})
					saveInProgress = true
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
					renderer.Recenter(mgl64.Vec2{
						float64(pos.X()),
						float64(pos.Z()),
					})
				}
			case *packet.SubChunk:
				go func() {
					for _, entry := range pk.SubChunkEntries {
						if entry.Result == protocol.SubChunkResultSuccess {
							offsetPos := world.ChunkPos{
								pk.Position.X() + int32(entry.Offset[0]),
								pk.Position.Z() + int32(entry.Offset[2]),
							}

							chunkMu.Lock()
							c, ok := chunks[offsetPos]
							if !ok {
								c = chunk.New(airRID, worldRange)
								chunks[offsetPos] = c
							}
							chunkMu.Unlock()

							var ind byte
							newSub, err := chunk.DecodeSubChunk(bytes.NewBuffer(entry.RawPayload), c, &ind, chunk.NetworkEncoding)
							if err == nil {
								chunkMu.Lock()
								c.Sub()[ind] = newSub
								chunkMu.Unlock()
							}

							renderer.RerenderChunk(offsetPos)
						}
					}
				}()
			case *packet.ChangeDimension:
				chunkMu.Lock()
				for chunkPos := range chunks {
					delete(chunks, chunkPos)
				}
				chunkMu.Unlock()

				renderer.Rerender()
			case *packet.LevelChunk:
				switch pk.SubChunkRequestMode {
				case protocol.SubChunkRequestModeLegacy:
					go func() {
						chunkPos := world.ChunkPos{pk.Position.X(), pk.Position.Z()}
						c, err := chunk.NetworkDecode(airRID, pk.RawPayload, int(pk.SubChunkCount), oldFormat, worldRange)
						if err == nil {
							chunkMu.Lock()
							chunks[chunkPos] = c
							chunkMu.Unlock()

							renderer.RerenderChunk(chunkPos)
						}
					}()
				}
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
	Downloader struct {
		OutputDirectory string
	}
}

// readConfig reads the configuration from the config.toml file, or creates the file if it does not yet exist.
func readConfig() (config, error) {
	c := config{}
	c.Connection.LocalAddress = ":19132"
	c.Connection.RemoteAddress = "play.lbsg.net:19132"
	if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
		data, err := toml.Marshal(c)
		if err != nil {
			return c, fmt.Errorf("failed encoding default config: %v", err)
		}
		if err := os.WriteFile("config.toml", data, 0644); err != nil {
			return c, fmt.Errorf("failed creating config: %v", err)
		}
		return c, nil
	}
	data, err := os.ReadFile("config.toml")
	if err != nil {
		return c, fmt.Errorf("error reading config: %v", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("error decoding config: %v", err)
	}
	return c, nil
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
