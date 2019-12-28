/*
 * Copyright (c) 2019 Zachariah Knight <aeros.storkpk@gmail.com>
 *
 * Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 *
 */

package packethandlers

import (
	"github.com/spkaeros/rscgo/pkg/server/log"
	"github.com/spkaeros/rscgo/pkg/server/packet"
	"github.com/spkaeros/rscgo/pkg/server/world"
)

func init() {
	//requiredLevels contains prayer level requirements for each prayer in order from prayer 0 to prayer 13
	requiredLevels := []int{1, 4, 7, 10, 13, 16, 19, 22, 25, 28, 31, 34, 37, 40}
	PacketHandlers["prayeron"] = func(player *world.Player, p *packet.Packet) {
		idx := p.ReadByte()
		if idx < 0 || idx > 13 {
			log.Suspicious.Printf("%v turned on a prayer that doesn't exist: %d\n", player, idx)
			return
		}
		if requiredLevels[idx] > player.Skills().Maximum(world.StatPrayer) {
			log.Suspicious.Printf("%v turned on a prayer that he is too low level for: %d\n", player, idx)
			return
		}
		player.PrayerOn(int(idx))
		player.SendPrayers()
	}
	PacketHandlers["prayeroff"] = func(player *world.Player, p *packet.Packet) {
		idx := p.ReadByte()
		if idx < 0 || idx > 13 {
			log.Suspicious.Printf("%v turned off a prayer that doesn't exist: %d\n", player, idx)
			return
		}
		if requiredLevels[idx] > player.Skills().Maximum(world.StatPrayer) {
			log.Suspicious.Printf("%v turned off a prayer that he is too low level for: %d\n", player, idx)
			return
		}
		if player.PrayerActivated(int(idx)) {
			player.PrayerOff(int(idx))
		}
		player.SendPrayers()
	}
}