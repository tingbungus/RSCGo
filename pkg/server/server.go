package server

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/gobwas/ws"
	"github.com/spkaeros/rscgo/pkg/server/players"

	"github.com/spkaeros/rscgo/pkg/server/config"
	"github.com/spkaeros/rscgo/pkg/server/log"
	"github.com/spkaeros/rscgo/pkg/server/world"
)

var (
	Kill = make(chan struct{})
)

func Bind(port int) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Error.Printf("Can't bind to specified port: %d\n", port)
		log.Error.Println(err)
		os.Exit(1)
	}

	go func() {
		var wsUpgrader = ws.Upgrader{
			Protocol: func(protocol []byte) bool {
				// Chrome is picky, won't work without explicit protocol acceptance
				return true
			},
		}

		defer func() {
			err := listener.Close()
			if err != nil {
				log.Error.Println("Could not close server socket listener:", err)
				return
			}
		}()

		for {
			socket, err := listener.Accept()
			if err != nil {
				if config.Verbosity > 0 {
					log.Error.Println("Error occurred attempting to accept a client:", err)
				}
				continue
			}
			if port == config.WSPort() {
				if _, err := wsUpgrader.Upgrade(socket); err != nil {
					log.Info.Println("Error upgrading websocket connection:", err)
					continue
				}
			}
			if players.Size() >= config.MaxPlayers() {
				if n, err := socket.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0, 14}); err != nil || n != 9 {
					if config.Verbosity > 0 {
						log.Error.Println("Could not send world is full response to rejected client:", err)
					}
				}
				continue
			}

			newClient(socket, port == config.WSPort())
		}
	}()
}

func StartConnectionService() {
	Bind(config.Port())   // UNIX sockets
	Bind(config.WSPort()) // websockets
}

//Tick One game engine 'tick'.  This is to handle movement, to synchronize client, to update movement-related state variables... Runs once per 600ms.
func Tick() {
	go func() {
		players.Range(func(p *world.Player) {
			if fn := p.DistancedAction; fn != nil {
				if fn() {
					p.ResetDistancedAction()
				}
			}
		})
	}()
	players.Range(func(p *world.Player) {
		p.TraversePath()
	})
	world.UpdateNPCPositions()
	// Giant lock for changing `sync` transient attribute on players
	// Occasionally, when other goroutines changed our sync status, the engines goroutine would be mid-update
	// This would cause strange positioning to occur since I do not send packets every tick but only when needed
	// There is no race, it's just that we end up resetting a sync state that never got sent because of it.
	// This fixes it by preventing any state changes during engine ticks.  This code runs for such a short time
	// that this should be fine performance-wise.
	world.GiantLock.Lock()
	players.Range(func(p *world.Player) {
		// Everything is updated relative to our player's position, so player position packet comes first
		if positions := world.PlayerPositions(p); positions != nil {
			p.SendPacket(positions)
		}
		if appearances := world.PlayerAppearances(p); appearances != nil {
			p.SendPacket(appearances)
		}
		if npcUpdates := world.NPCPositions(p); npcUpdates != nil {
			p.SendPacket(npcUpdates)
		}
		if itemUpdates := world.ItemLocations(p); itemUpdates != nil {
			p.SendPacket(itemUpdates)
		}
		if objectUpdates := world.ObjectLocations(p); objectUpdates != nil {
			p.SendPacket(objectUpdates)
		}
		if boundaryUpdates := world.BoundaryLocations(p); boundaryUpdates != nil {
			p.SendPacket(boundaryUpdates)
		}
	})
	players.Range(func(p *world.Player) {
		if time.Since(p.Transients().VarTime("deathTime")) < 5*time.Second {
			// Ugly hack to work around a client bug with region loading.
			return
		}
		p.ResetNeedsSelf()
		p.ResetChanged()
		p.ResetMoved()
		p.ResetRemoved()
	})
	world.ResetNpcUpdateFlags()
	world.GiantLock.Unlock()
}

//StartGameEngine Launches a goroutine to handle updating the state of the server every 600ms in a synchronized fashion.  This is known as a single game engine 'pulse'.
func StartGameEngine() {
	go func() {
		ticker := time.NewTicker(600 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			Tick()
		}
	}()
}

//Stop This will stop the server instance, if it is running.
func Stop() {
	log.Info.Println("Stopping server...")
	Kill <- struct{}{}
}
