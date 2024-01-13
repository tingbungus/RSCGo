/*
 * Copyright (c) 2020 Zachariah Knight <aeros.storkpk@gmail.com>
 *
 * Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 *
 */

package main

import (
	"crypto/tls"
	stdnet "net"
	"os"
	"runtime"
	"context"
	"sync"
	"strconv"
	"time"
	"strings"
	"bufio"
	"encoding/binary"
	"math"
	"crypto/rand"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/jessevdk/go-flags"
	"github.com/BurntSushi/toml"

	"github.com/spkaeros/rscgo/pkg/crypto"
	"github.com/spkaeros/rscgo/pkg/config"
	"github.com/spkaeros/rscgo/pkg/definitions"
	"github.com/spkaeros/rscgo/pkg/tasks"
	rscerrors "github.com/spkaeros/rscgo/pkg/errors"
	"github.com/spkaeros/rscgo/pkg/db"
	"github.com/spkaeros/rscgo/pkg/isaac"
	"github.com/spkaeros/rscgo/pkg/xtea"
	"github.com/spkaeros/rscgo/pkg/rsa"
	rscrand "github.com/spkaeros/rscgo/pkg/rand"
	"github.com/spkaeros/rscgo/pkg/strutil"
	"github.com/spkaeros/rscgo/pkg/game/net"
	"github.com/spkaeros/rscgo/pkg/game/net/handshake"
	"github.com/spkaeros/rscgo/pkg/game/world"
	"github.com/spkaeros/rscgo/pkg/log"
	
)

type sigEnd chan struct{}

const (
	TickMillis = time.Millisecond*640
)
//run Helper function for concurrently running a bunch of functions and waiting for them to complete
func run(fns ...func()) {
	w := &sync.WaitGroup{}
	do := func(fn func()) {
		w.Add(1)
		go func(fn func()) {
			defer w.Done()
			fn()
		}(fn)
	}

	for _, fn := range fns {
		do(fn)
	}
	w.Wait()
}

type (
	Flags struct {
		Verbose   []bool `short:"v" long:"verbose" description:"Display more verbose output"`
		Port      int    `short:"p" long:"port" description:"The TCP port for the game to listen on, (Websocket will use the port directly above it)"`
		Config    string `short:"c" long:"config" description:"Specify the TOML configuration file to load game settings from" default:"config.toml"`
		UseCipher bool   `short:"e" long:"encryption" description:"Enable command opcode encryption using a variant of ISAAC to encrypt net opcodes."`
	}
	Server struct {
		context.Context
		loginQ chan *world.Player
		logoutQ chan *world.Player
		sync.RWMutex
		*time.Ticker
		port  int
		debug bool
		*tasks.Scripts
	}
)

var (
	cliFlags = &Flags{}
	start = time.Now()
	tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{
			check(tls.LoadX509KeyPair("../data/ssl/fullchain.pem", "../data/ssl/privkey.pem")).(tls.Certificate),
		},
		ServerName: "rsclassic.dev",
		InsecureSkipVerify: false,
		SessionTicketsDisabled: true,
		PreferServerCipherSuites: true,
		// ClientAuth: tls.RequireAndVerifyClientCert,
		ClientAuth: tls.NoClientCert,
		Rand: rand.Reader,
	}
	wsUpgrader = ws.Upgrader{
		Protocol: func(protocol []byte) bool {
			return string(protocol) == "binary"
		},
		ReadBufferSize:  5000,
		WriteBufferSize: 5000,
	}
)
func openUserDatabase()  {
	run(func() {
		world.DefaultPlayerService = db.NewPlayerServiceSql()
	}, func() {
		db.DefaultPlayerService = db.NewPlayerServiceSql()
	})
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU() * 2)
	// runtime.GOMAXPROCS(1)
	if check(flags.Parse(cliFlags)) == nil {
		log.Warn("Error parsing command arguments!")
		os.Exit(1)
		return
	}
	if len(cliFlags.Config) == 0 {
		// Default to config.toml for config file
		cliFlags.Config = "config.toml"
	}

	config.TomlConfig.MaxPlayers = 1250
	config.TomlConfig.DataDir = "./data/"
	config.TomlConfig.DbioDefs = config.TomlConfig.DataDir + "dbio.conf"
	config.TomlConfig.PacketHandlerFile = config.TomlConfig.DataDir + "packets.toml"
	config.TomlConfig.Crypto.HashComplexity = 15
	config.TomlConfig.Crypto.HashLength = 32
	config.TomlConfig.Crypto.HashMemory = 8
	config.TomlConfig.Crypto.HashSalt = "rscgo./GOLANG!RULES/.1994"
	config.TomlConfig.Version = 235
	config.TomlConfig.Port = 43594 // +1 for websockets
	// TODO: data backend default to JSON or BSON maybe?
	config.TomlConfig.Database.PlayerDriver = "sqlite3"
	config.TomlConfig.Database.WorldDriver = "sqlite3"
	config.TomlConfig.Database.PlayerDB = "file:./data/players.db"
	config.TomlConfig.Database.WorldDB = "file:./data/world.db"
	if _, err := toml.DecodeFile(cliFlags.Config, &config.TomlConfig); err != nil {
		log.Fatal("Error decoding server config (file:%s):", err)
		os.Exit(2)
		return
	}
	if _, err := toml.DecodeFile(config.TomlConfig.DbioDefs, &config.TomlConfig.Database); err != nil {
		log.Fatal("Error decoding database i/o config (file:"+config.TomlConfig.DbioDefs+"):", err)
		os.Exit(3)
		return
	}
	run(db.ConnectEntityService, openUserDatabase)
	if cliFlags.Port > 0 {
		config.TomlConfig.Port = cliFlags.Port
	}
	if config.Port() >= 65534 || config.Port() < 0 {
		log.Warn("Error: Invalid port number specified.")
		log.Warn("Valid port numbers are 1-65533 (needs the port 1 above it open to bind a websockets listener).")
		return 
	}
	config.Verbosity = int(math.Min(math.Max(float64(len(cliFlags.Verbose)), 0), 4))
	// Three init phases after data backend is connected--Entity definitions, then tile collision bitmask loading, followed by entity spawn locations
	// So, the order here of these three phases is important.  If you attempt to load object spawn locations during the same phase as the collision
	// data, it will result in a world filled with objects that are not solid.  Many similar bugs possible.  Best just to leave this be.
	run(db.LoadTileDefinitions, db.LoadObjectDefinitions, db.LoadBoundaryDefinitions, db.LoadItemDefinitions, db.LoadNpcDefinitions)
	run(world.LoadCollisionData, world.UnmarshalPackets, world.RunScripts)
		// world.LoadCollisionData, world.UnmarshalPackets, world.RunScripts)
	run(db.LoadObjectLocations, db.LoadNpcLocations, db.LoadItemLocations)

	if config.Verbose() {
		log.Debug("Loaded collision data from", len(world.Sectors), "map sectors")
		log.Debug("Loaded", len(definitions.TileOverlays), "tile types")
		log.Debug("Loaded", world.PacketCount(), "packet types, with handlers for", world.HandlerCount(), "of them")
		log.Debug("Loaded", world.ItemIndexer.Load(), "items and", len(definitions.Items), "item types")
		log.Debug("Loaded", world.Npcs.Size(), "NPCs and", len(definitions.Npcs), "NPC types")
		scenary, boundary := 0, 0
		for _, v := range world.GetAllObjects() {
			if v.(*world.Object).Boundary {
				boundary++
			} else {
				scenary++
			}
		}
		log.Debug("Loaded", scenary, "scenary objects, and", len(definitions.ScenaryObjects), "scenary types.")
		log.Debug("Loaded", boundary, "boundary objects, and", len(definitions.BoundaryObjects), "boundary types")
		log.Debug("Loading all game entitys took:", time.Since(start).Seconds(), "seconds")
		if config.Verbosity >= 2 {
			log.Debugf("Triggers[\n\t%d item actions,\n\t%d scenary actions,\n\t%d boundary actions,\n\t%d npc actions,\n\t%d item->boundary actions,\n\t%d item->scenary actions,\n\t%d attacking NPC actions,\n\t%d killing NPC actions\n];\n", len(world.ItemTriggers), len(world.ObjectTriggers), len(world.BoundaryTriggers), len(world.NpcTalkList), len(world.InvOnBoundaryTriggers), len(world.InvOnObjectTriggers), len(world.NpcAtkTriggers), len(world.NpcDeathTriggers))
		}
	}
	log.Debug("Listening at TCP port " + strconv.Itoa(config.Port()))// + " (TCP), " + strconv.Itoa(config.WSPort()) + " (websockets)")
	log.Debug()
	log.Debug("RSCGo has finished initializing world; we hope you enjoy it")
	// go Instance.WsBind()
	go Instance.Start()
	Instance.Bind(config.Port())
	select{}
}

func needsData(err error) bool {
	return err.Error() == "Socket buffer has less bytes available than we need to form a message packet."
}

var Instance = &Server{Ticker: time.NewTicker(world.TickMillis), loginQ: make(chan *world.Player, 25), logoutQ: make(chan *world.Player, 25), Scripts: tasks.TickList}

func (s *Server) accept(l stdnet.Listener) *world.Player {
	socket, err := l.Accept()
	if err != nil {
		log.Warn("Problem accepting incoming TLS connection from '" + socket.RemoteAddr().String() + "':", err)
		return nil
	}
	p := world.NewPlayerCtx(Instance, socket)
	if str := socket.LocalAddr().String(); strings.HasSuffix(str, strconv.Itoa(s.port)) {
		p.Websocket = true
		p.Reader = bufio.NewReaderSize(wsutil.NewServerSideReader(socket), 5000)
		p.Writer = wsutil.NewWriterSize(p.Socket, ws.StateServerSide, ws.OpBinary, 5000)
		if check(wsUpgrader.Upgrade(socket)) == nil {
			log.Debug("could not upgrade to websocket")
			return nil
		}
	} else {
		p.Websocket = false
		p.Reader = bufio.NewReaderSize(p.Socket, 5000)
		p.Writer = bufio.NewWriterSize(p.Socket, 5000)
	}
	// p.Websocket = true
	// log.Debug(p.Socket)
	return p
}

func (s *Server) Bind(port int) {
	s.port = port
	bindTo := func(listener stdnet.Listener) {
		for {
			if p := s.accept(listener); p != nil {
				s.SubmitLogin(p)
			}
		}
	}
	go bindTo(check(stdnet.Listen("tcp", ":" + strconv.Itoa(port+1))).(stdnet.Listener))
	go bindTo(tls.NewListener(check(stdnet.Listen("tcp", ":" + strconv.Itoa(port))).(stdnet.Listener), tlsConfig))
}

func (s *Server) Start() {
	defer s.Ticker.Stop()
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), "server", Instance))
	s.Context = ctx
	defer cancel()
	// s.DebugTicks()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.C:
			start := time.Now()
			select {
			case <-ctx.Done():
				return
			default:
				for i := 0; i < 25; i++ { 
					select {
					case p1, ok := <-s.loginQ:
						if !ok || p1 == nil {
							break
						}
						s.handleLogin(p1)
					default:
						break
					}
				}
				
				world.Players.AsyncRange(func(p *world.Player) {
					if p == nil {
						return
					}
					p.ProcPacketsIn() // dequeue incoming packets.  These are read off the socket then queued by each players own goroutine
					if !p.Connected() {
						p.Initialize()
					}
					if fn := p.TickAction(); fn != nil && !fn() {
						p.ResetTickAction()
					}
					p.TraversePath()
				})
				world.Npcs.RangeNpcs(func(n *world.NPC) bool {
					if n.Busy() || n.IsFighting() {
						return false
					}

					if n.Aggressive() {
						if _, ok := n.Var("targetPlayer"); ok {
							return false
						}
						if closest := closestPlayer(n); closest != nil {
							n.SetVar("targetPlayer", closest)
						}
						n.TraversePath()
						return false
					}
					if world.Chance(25) && n.Steps <= 0 && n.Ticks <= 0 {
						// move some amount between 2-15 tiles, moving 1 tile per tick
						n.Steps = rscrand.Intn(13+1)+2
						// wait some amount between 25-50 ticks before doing this again
						n.Ticks = rscrand.Intn(10+1)+25
					}
					if n.Ticks > 0 {
						n.Ticks -= 1
					}
					// wander aimlessly until we run out of scheduled steps
					if n.Steps > 0 {
						n.TraversePath()
					}
					return false
				})
				s.Tick(ctx)
				world.Players.AsyncRange(func(p *world.Player) {
					sendPacket := func(p1 *net.Packet) {
						if p != nil && p1 != nil {
							p.WritePacket(p1)// <- p1
						}
					}
					// if p.HasState(world.StateChangingLooks) {
						// sendPacket(world.AppearanceKeepalive)
						// return
					// }
					sendPacket(world.PlayerPositions(p))
					sendPacket(world.NPCPositions(p))
					sendPacket(world.PlayerAppearances(p))
					sendPacket(world.NpcEvents(p))
					sendPacket(world.ObjectLocations(p))
					sendPacket(world.BoundaryLocations(p))
					sendPacket(world.ItemLocations(p))
					sendPacket(world.ClearDistantChunks(p))
					if p.VarInt("lastPlane", -1) != p.Plane() {
						sendPacket(world.PlaneInfo(p))
						p.SetVar("lastPlane", p.Plane())
					}
				})

				world.Players.AsyncRange(func(p *world.Player) {
					p.ResetRegionRemoved()
					p.ResetRegionMoved()
					p.ResetSpriteUpdated()
					p.ResetAppearanceChanged()
					p.ProcPacketsOut() // dequeue incoming packets.  These are read off the socket then queued by each players own goroutine
				})
				world.Npcs.RangeNpcs(func(n *world.NPC) bool {
					n.ResetRegionRemoved()
					n.ResetRegionMoved()
					n.ResetSpriteUpdated()
					n.ResetAppearanceChanged()
					return false
				})
				for i := 0; i < 25; i++ { 
					select {
					case p1, ok := <-s.logoutQ:
						if !ok || p1 == nil {
							break
						}
						s.runLogout(p1)
					default:
						break
					}
				}
				if s.debug {
					// if world.CurrentTick() % 100 == 0 {
						// each 64 seconds we log our tick processing time
						log.Debug("time to process tick:", time.Since(start))
					// }
				}
			}
		}
	}
}

//Stop This will stop the game instance, if it is running.
func (s *Server) Stop() {
	log.Debug("Stopping...")
	os.Exit(0)
}

func check(i interface{}, err error) interface{} {
	if err != nil {
		log.Debug("Error encountered:", err)
		return nil
	}
	return i
}
		
func closestPlayer(n *world.NPC) *world.Player {
	var closest *world.Player
	distance := math.Pow(8.0,2)
	world.Region(n.X(), n.Y()).Players.RangePlayers(func(p1 *world.Player) bool {
		if p1.EuclideanDistance(n) < distance {
			closest = p1
			distance = p1.EuclideanDistance(n)
		}
		return false
	})
	return closest
}

func (s *Server) handleLogin(p *world.Player) {
	login, err := p.ReadPacket()
	if login == nil {
		return
	}
	if err != nil && err.(rscerrors.NetError).Fatal {
		p.Unregister()
		return
	}
	sendReply := func(i handshake.ResponseCode, reason string) {
		p.Writer.Write([]byte{byte(i)})
		p.Writer.Flush()
		if !i.IsValid() {
			log.Debug("[LOGIN]", p.Username() + "@" + p.CurrentIP(), "failed to login (" + reason + ")")
			p.Unregister()
		} else {
			log.Debug("[LOGIN]", p.Username() + "@" + p.CurrentIP(), "successfully logged in")
			go func() {
				defer p.Unregister()
				world.AddPlayer(p)
				for {
					select {
					case <-p.Done():
						return
					default:
						packet, err := p.ReadPacket()
						if err != nil || packet == nil {
							if err.(rscerrors.NetError).Fatal {
								return
							}
						}
						p.InQueue <- packet
						
					}
				}
			}()
			// p.Initialize()
		}
	}

	if login.Opcode != 0 {
		log.Debug("Unhandled login packet from", p, ":", login.String())
		return
	}
	if !world.UpdateTime.IsZero() {
		sendReply(handshake.ResponseServerRejection, "System update in progress")
		return
	}
	if world.Players.Size() >= config.MaxPlayers() {
		sendReply(handshake.ResponseWorldFull, "Out of usable player slots")
		return
	}
	if handshake.LoginThrottle.Recent(p.CurrentIP(), time.Second*10) >= 5 {
		sendReply(handshake.ResponseSpamTimeout, "Too many recent invalid login attempts (5 in 10 seconds)")
		return
	}

	p.SetReconnecting(login.ReadBoolean())
	if ver := login.ReadUint32(); ver != config.Version() {
		sendReply(handshake.ResponseUpdated, "Invalid client version (" + strconv.Itoa(ver) + ")")
		return
	}

	rsaSize := login.ReadUint16()
	data := make([]byte, rsaSize)
	rsaRead := login.Read(data)
	if rsaRead < rsaSize {
		log.Debug("short RSA block")
		p.Writer.Write([]byte{byte(handshake.ResponseServerRejection)})
		p.Writer.Flush()
		return
	} 

	rsaData := rsa.RsaKeyPair.Decrypt(data)
	offset := 0
	checksum := rsaData[offset]
	offset++
	// It's been suggested to me that this first byte assures us that the RSA block could decode properly,
	// it's only wrong for this purpose a statistically insignificant amount of time.  >99% accurate, as I understand it.
	if checksum != 10 {
		log.Debug("Bad checksum:", checksum)
		p.Writer.Write([]byte{byte(handshake.ResponseServerRejection)})
		p.Writer.Flush()
		return
	}
	var keys = make([]int, 4)
	for i := range keys {
		keys[i] = int(binary.BigEndian.Uint32(rsaData[offset:]))
		offset += 4
	}
	p.OpCiphers[0] = isaac.New(keys...)
	p.OpCiphers[1] = isaac.New(keys...)
	// protocol pads password out to constant 19 chars long (+1 terminator) for some reason with 0x20 bytes
	password := strings.TrimSpace(string(rsaData[offset:offset+19]))
	offset += 20
	// The rscplus team viewed this data below as a nonce, but in my opinion, this is not the motivation for this data.
	// I'd call these more of an initialization vector (IV), as wikipedia defines it, used to make RSA semantically secure.
	offset += 8
	blockSize := login.ReadUint16()
	var block = make([]byte, blockSize)
	if login.Available() != blockSize {
		log.Debug("XTEA block size recv'd doesn't take up the rest of the packets available buffer size! (it should)")
		log.Debugf("\t{ blockSize:%d, login.Available():%d }\n", blockSize, login.Available())
	}
	login.Read(block)
	offset = 0
	// limit30 := block[offset]
	offset++
	usernameData := xtea.New(keys).Decrypt(block)
	offset += 24
	// first byte of this block is limit30 parameter from the game client applet; boolean, use unknown
	// I suppose the next 24 bytes are to ensure the stream gets sufficiently shuffled in each packet, preventing identifying markers appearing
	// finally, the null-terminated UTF-8 encoded username comes at offset 25 and beyond.
	username := string(usernameData[25:])
	p.SetVar("username", strutil.Base37.Encode(username))
	if world.Players.ContainsHash(p.UsernameHash()) {
		sendReply(handshake.ResponseLoggedIn, "Player with same username is already logged in")
		return
	}
	var dataService = db.DefaultPlayerService
	if !dataService.PlayerNameExists(p.Username()) || !dataService.PlayerValidLogin(p.UsernameHash(), crypto.Hash(password)) {
		handshake.LoginThrottle.Add(p.CurrentIP())
		sendReply(handshake.ResponseBadPassword, "Invalid credentials")
		return
	}
	if !dataService.PlayerLoad(p) {
		sendReply(handshake.ResponseDecodeFailure, "Could not load player profile; is the dataService setup properly?")
		return
	}

	if p.Reconnecting() {
		sendReply(handshake.ResponseReconnected, "")
		return
	}
	switch p.Rank() {
	case 2:
		sendReply(handshake.ResponseAdministrator|handshake.ResponseLoginAcceptBit, "")
	case 1:
		sendReply(handshake.ResponseModerator|handshake.ResponseLoginAcceptBit, "")
	default:
		sendReply(handshake.ResponseLoginSuccess|handshake.ResponseLoginAcceptBit, "")
	}
	return
}

func (s *Server) SubmitLogout(p *world.Player) {
	// s.Lock()
	// s.logoutQueue = append(s.logoutQueue, p)
	s.logoutQ <- p
	// s.Unlock()
}

func (s *Server) SubmitLogin(p *world.Player) {
	// s.Lock()
	s.loginQ <- p
	// s.loginQueue = append(s.loginQueue, p)
	// s.Unlock()
}

func (s *Server) DebugTicks() {
	s.debug = !s.debug
}

func (s *Server) runLogout(p *world.Player) {
	go p.Destroy()
}

func (s *Server) WrapWaitRoutines (ps []*world.Player, fns ...func(*world.Player)) ( func() ) {
	if len(ps) == 0 {
		return nil
	}
	return func() {
		wait := sync.WaitGroup{}
		for _, p := range ps {
			select {
			case <-p.Done():
				return
			default:
				for _, fn := range fns {
					if p == nil {
						return
					}
				
				// case <-p.Done():
					// return
					wait.Add(1)
					go func() {
						defer wait.Done()
						fn(p)
					}()
				}
			}
		}
		// select {
			// case <-ctx.Done():
				// return
			// default:
				// for _, fn := range fns {
					// go fn()
				// }
				// // world.Players.Range(func(p *world.Player) {
					// // p.ProcPacketsIn() // dequeue incoming packets.  These are read off the socket then queued by each players own goroutine
				// // })
		// }
		wait.Wait()
	}
	// })(context.WithCancel(ctx))
}
