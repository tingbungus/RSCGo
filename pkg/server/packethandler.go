package server

// TODO: Maybe load this from some sort of persistent storage medium, e.g YAML/TOML/JSON file
const (
	LoginRequest   = 0
	SessionRequest = 32
)

var handlers = make(map[byte]func(*Client, *Packet))

func sessionRequest(c *Client, p *Packet) {
	c.uID = p.payload[0]
	p1 := &Packet{bare: true}
	seed := GenerateSessionID()
	c.isaacSeed[2] = int32(seed >> 32)
	c.isaacSeed[3] = int32(seed)
	p1.AddLong(seed)
	c.WritePacket(p1)
}

func loginRequest(c *Client, p *Packet) {
	if err := p.DecryptRSA(); err != nil {
		LogDebug(1, "WARNING: Could not decrypt RSA login block.\n")
		c.sendLoginResponse(9)
		return
	}
	// TODO: Handle reconnect slightly different
	recon, version := p.ReadByte() == 1, int(p.ReadInt())
	if version != Version {
		LogDebug(1, "WARNING: Player tried logging in with invalid client version. Got %d, expected %d\n", version, Version)
		c.sendLoginResponse(5)
		return
	}
	var seed [4]int32
	for i := 0; i < 4; i++ {
		seed[i] = int32(p.ReadInt())
	}
	if seed[2] != c.isaacSeed[2] || seed[3] != c.isaacSeed[3] {
		LogDebug(1, "WARNING: Session encryption key for command cipher received from client doesn't match the one we supplied it.\n")
		c.sendLoginResponse(8)
		return
	}
	c.isaacSeed[0] = seed[0]
	c.isaacSeed[1] = seed[1]
	username, password := p.ReadString(), p.ReadString()
	LogDebug(0, "Registered Player{username:%v,password:%v,reconnecting:%v,version:%v,clientSeed:%v,serverSeed:%v}\n", username, password, recon, version, int64(seed[0]) << 32 | int64(seed[1]), int64(seed[2]) << 32 | int64(seed[3]))
	c.sendLoginResponse(0)
}

func (c *Client) sendLoginResponse(i int) {
	c.WritePacket(&Packet{bare: true, payload:[]byte{byte(i)}})
	if i != 0 {
		c.kill <- struct{}{}
	}
}

func ping(c *Client, p *Packet) {
	c.WritePacket(&Packet{opcode: 3, length: 0, payload: []byte{}})
}

func init() {
	handlers[32] = sessionRequest
	handlers[0] = loginRequest
	handlers[5] = ping
}

func (c *Client) HandlePacket(p *Packet) {
	handler, ok := handlers[p.opcode]
	if !ok {
		LogDebug(0, "Unhandled Packet: {opcode:%d; length:%d};\n", p.opcode, p.length)
		return
	}
	handler(c, p)
}
