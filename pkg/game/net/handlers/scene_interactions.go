/*
 * Copyright (c) 2020 Zachariah Knight <aeros.storkpk@gmail.com>
 *
 * Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 *
 */

package handlers

import (
	"github.com/spkaeros/rscgo/pkg/game/net"
	"github.com/spkaeros/rscgo/pkg/game/world"
	"github.com/spkaeros/rscgo/pkg/log"
)

func init() {
	AddHandler("objectaction", func(player *world.Player, p *net.Packet) {
		if player.Busy() {
			return
		}
		x := p.ReadShort()
		y := p.ReadShort()
		object := world.GetObject(x, y)
		if object == nil || object.Boundary {
			log.Suspicious.Printf("Player %v attempted to use a non-existent object at %d,%d\n", player, x, y)
			return
		}
		player.SetDistancedAction(func() bool {
			if player.AtObject(object) {
				player.ResetPath()
				player.AddState(world.MSBusy)

				go func() {
					defer func() {
						player.RemoveState(world.MSBusy)
					}()

					for _, trigger := range world.ObjectTriggers {
						if trigger.Check(object, 0) {
							trigger.Action(player, object, 0)
							return
						}
					}
					player.SendPacket(world.DefaultActionMessage)
				}()
				return true
			}
			return false

		})
	})
	AddHandler("objectaction2", func(player *world.Player, p *net.Packet) {
		if player.Busy() {
			return
		}
		x := p.ReadShort()
		y := p.ReadShort()
		object := world.GetObject(x, y)
		if object == nil || object.Boundary {
			log.Suspicious.Printf("Player %v attempted to use a non-existent object at %d,%d\n", player, x, y)
			return
		}
		player.SetDistancedAction(func() bool {
			if player.AtObject(object) {
				player.ResetPath()
				player.AddState(world.MSBusy)

				go func() {
					defer func() {
						player.RemoveState(world.MSBusy)
					}()

					for _, trigger := range world.ObjectTriggers {
						if trigger.Check(object, 1) {
							trigger.Action(player, object, 1)
							return
						}
					}
					player.SendPacket(world.DefaultActionMessage)
				}()

				return true
			}
			return false
		})
	})
	AddHandler("boundaryaction2", func(player *world.Player, p *net.Packet) {
		if player.Busy() {
			return
		}
		x := p.ReadShort()
		y := p.ReadShort()
		object := world.GetObject(x, y)
		if object == nil || !object.Boundary {
			log.Suspicious.Printf("Player %v attempted to use a non-existent boundary at %d,%d\n", player, x, y)
			return
		}
		bounds := object.Boundaries()
		player.SetDistancedAction(func() bool {
			if (player.NextTo(bounds[1]) || player.NextTo(bounds[0])) && player.X() >= bounds[0].X() && player.Y() >= bounds[0].Y() && player.X() <= bounds[1].X() && player.Y() <= bounds[1].Y() {
				player.ResetPath()
				if player.Busy() || world.GetObject(object.X(), object.Y()) != object {
					// If somehow we became busy, the object changed before arriving, we do nothing.
					return true
				}
				player.AddState(world.MSBusy)
				go func() {
					defer func() {
						player.RemoveState(world.MSBusy)
					}()

					for _, trigger := range world.BoundaryTriggers {
						if trigger.Check(object, 1) {
							trigger.Action(player, object, 1)
							return
						}
					}
					player.SendPacket(world.DefaultActionMessage)
				}()
				return true
			}
			return false
		})
	})
	AddHandler("boundaryaction", func(player *world.Player, p *net.Packet) {
		if player.Busy() {
			return
		}
		x := p.ReadShort()
		y := p.ReadShort()
		object := world.GetObject(x, y)
		if object == nil || !object.Boundary {
			log.Suspicious.Printf("%v attempted to use a non-existent boundary at %d,%d\n", player, x, y)
			return
		}
		bounds := object.Boundaries()
		player.SetDistancedAction(func() bool {
			if (player.NextTo(bounds[1]) || player.NextTo(bounds[0])) && player.X() >= bounds[0].X() && player.Y() >= bounds[0].Y() && player.X() <= bounds[1].X() && player.Y() <= bounds[1].Y() {
				player.ResetPath()
				if player.Busy() || world.GetObject(object.X(), object.Y()) != object {
					// If somehow we became busy, the object changed before arriving, we do nothing.
					return true
				}
				player.AddState(world.MSBusy)
				go func() {
					defer func() {
						player.RemoveState(world.MSBusy)
					}()

					for _, trigger := range world.BoundaryTriggers {
						if trigger.Check(object, 0) {
							trigger.Action(player, object, 0)
							return
						}
					}
					player.SendPacket(world.DefaultActionMessage)
				}()
				return true
			}
			return false
		})
	})
	AddHandler("talktonpc", func(player *world.Player, p *net.Packet) {
		idx := p.ReadShort()
		npc := world.GetNpc(idx)
		if npc == nil {
			return
		}
		if player.IsFighting() {
			return
		}
		player.WalkingArrivalAction(npc, 1, func() {
			player.ResetPath()
			if npc.Busy() {
				player.Message(npc.Name() + " is busy at the moment")
				return
			}
			if player.Busy() {
				return
			}
			for _, triggerDef := range world.NpcTriggers {
				if triggerDef.Check(npc) {
					npc.ResetPath()
					if player.Location.Equals(npc.Location) {
					outer:
						for offX := -1; offX <= 1; offX++ {
							for offY := -1; offY <= 1; offY++ {
								if offX == 0 && offY == 0 {
									continue
								}
								newLoc := world.NewLocation(player.X()+offX, player.Y()+offY)
								switch player.DirectionTo(newLoc.X(), newLoc.Y()) {
								case world.North:
									if world.IsTileBlocking(newLoc.X(), newLoc.Y(), world.ClipSouth, false) {
										continue
									}
								case world.South:
									if world.IsTileBlocking(newLoc.X(), newLoc.Y(), world.ClipNorth, false) {
										continue
									}
								case world.East:
									if world.IsTileBlocking(newLoc.X(), newLoc.Y(), world.ClipWest, false) {
										continue
									}
								case world.West:
									if world.IsTileBlocking(newLoc.X(), newLoc.Y(), world.ClipEast, false) {
										continue
									}
								case world.NorthWest:
									if world.IsTileBlocking(player.X(), player.Y()-1, world.ClipSouth, false) {
										continue
									}
									if world.IsTileBlocking(player.X()+1, player.Y(), world.ClipEast, false) {
										continue
									}
								case world.NorthEast:
									if world.IsTileBlocking(player.X(), player.Y()-1, world.ClipSouth, false) {
										continue
									}
									if world.IsTileBlocking(player.X()-1, player.Y(), world.ClipWest, false) {
										continue
									}
								case world.SouthWest:
									if world.IsTileBlocking(player.X(), player.Y()+1, world.ClipNorth, false) {
										continue
									}
									if world.IsTileBlocking(player.X()+1, player.Y(), world.ClipEast, false) {
										continue
									}
								case world.SouthEast:
									if world.IsTileBlocking(player.X(), player.Y()+1, world.ClipNorth, false) {
										continue
									}
									if world.IsTileBlocking(player.X()-1, player.Y(), world.ClipWest, false) {
										continue
									}
								}
								if player.NextTo(newLoc) {
									npc.SetLocation(newLoc, true)
									break outer
								}
							}
						}
					}

					if !player.Location.Equals(npc.Location) {
						player.SetDirection(player.DirectionTo(npc.X(), npc.Y()))
						npc.SetDirection(npc.DirectionTo(player.X(), player.Y()))
					}
					go func() {
						defer func() {
							player.RemoveState(world.MSChatting)
							npc.RemoveState(world.MSChatting)
						}()
						player.AddState(world.MSChatting)
						npc.AddState(world.MSChatting)
						triggerDef.Action(player, npc)
					}()
					return
				}
			}
			player.Message("The " + npc.Name() + " does not appear interested in talking")
		})
	})
	AddHandler("invonboundary", func(player *world.Player, p *net.Packet) {
		targetX := p.ReadShort()
		targetY := p.ReadShort()
		p.ReadByte() // dir, useful?
		invIndex := p.ReadShort()

		object := world.GetObject(targetX, targetY)
		if object == nil || !object.Boundary {
			log.Suspicious.Printf("%v attempted to use a non-existent boundary at %d,%d\n", player, targetX, targetY)
			return
		}
		if invIndex >= player.Inventory.Size() {
			log.Suspicious.Printf("%v attempted to use a non-existent item(idx:%v, cap:%v) on a boundary at %d,%d\n", player, invIndex, player.Inventory.Size()-1, targetX, targetY)
			return
		}
		invItem := player.Inventory.Get(invIndex)
		bounds := object.Boundaries()
		player.SetDistancedAction(func() bool {
			if player.Busy() || world.GetObject(object.X(), object.Y()) != object {
				// If somehow we became busy, the object changed before arriving, we do nothing.
				return true
			}
			if (player.NextTo(bounds[1]) || player.NextTo(bounds[0])) && player.X() >= bounds[0].X() && player.Y() >= bounds[0].Y() && player.X() <= bounds[1].X() && player.Y() <= bounds[1].Y() {
				player.ResetPath()
				player.AddState(world.MSBusy)
				go func() {
					defer func() {
						player.RemoveState(world.MSBusy)
					}()
					for _, fn := range world.InvOnBoundaryTriggers {
						if fn(player, object, invItem) {
							return
						}
					}
					player.SendPacket(world.DefaultActionMessage)
				}()
				return true
			}
			player.WalkTo(object.Location)
			return false
		})
	})
	AddHandler("invonplayer", func(player *world.Player, p *net.Packet) {
		targetIndex := p.ReadShort()
		invIndex := p.ReadShort()

		if targetIndex == player.Index {
			log.Suspicious.Printf("%s attempted to use an inventory item on themself\n", player.String())
			return
		}

		target, ok := world.Players.FromIndex(targetIndex)
		if !ok || target == nil || !target.Connected() {
			log.Suspicious.Printf("%s attempted to use an inventory item on a player that doesn't exist\n", player.String())
			return
		}
		if invIndex >= player.Inventory.Size() {
			log.Suspicious.Printf("%s attempted to use a non-existent item(idx:%v, cap:%v)  on a player(%s)\n", player.String(), invIndex, player.Inventory.Size()-1, target.String())
			return
		}
		invItem := player.Inventory.Get(invIndex)
		player.SetDistancedAction(func() bool {
			if player.Busy() || !player.Connected() || target == nil || target.Busy() || !target.Connected() {
				return true
			}
			if player.WithinRange(target.Location, 1) && player.NextTo(target.Location) {
				player.ResetPath()
				player.AddState(world.MSBusy)
				target.AddState(world.MSBusy)
				go func() {
					defer func() {
						player.RemoveState(world.MSBusy)
						target.RemoveState(world.MSBusy)
					}()
					for _, trigger := range world.InvOnPlayerTriggers {
						if trigger.Check(invItem) {
							trigger.Action(player, target, invItem)
							return
						}
					}
					player.SendPacket(world.DefaultActionMessage)
				}()
				return true
			}
			player.WalkTo(target.Location)
			return false
		})
	})
	AddHandler("invonobject", func(player *world.Player, p *net.Packet) {
		targetX := p.ReadShort()
		targetY := p.ReadShort()
		invIndex := p.ReadShort()

		object := world.GetObject(targetX, targetY)
		if object == nil || object.Boundary {
			log.Suspicious.Printf("%v attempted to use a non-existent boundary at %d,%d\n", player, targetX, targetY)
			return
		}
		if invIndex >= player.Inventory.Size() {
			log.Suspicious.Printf("%v attempted to use a non-existent item(idx:%v, cap:%v) on a boundary at %d,%d\n", player, invIndex, player.Inventory.Size()-1, targetX, targetY)
			return
		}
		invItem := player.Inventory.Get(invIndex)
		bounds := object.Boundaries()
		player.WalkTo(object.Location)
		player.SetDistancedAction(func() bool {
			if player.Busy() || world.GetObject(object.X(), object.Y()) != object {
				// If somehow we became busy, the object changed before arriving, we do nothing.
				return true
			}
			if world.ObjectDefs[object.ID].Type == 2 || world.ObjectDefs[object.ID].Type == 3 {
				if (player.NextTo(bounds[1]) || player.NextTo(bounds[0])) && player.X() >= bounds[0].X() && player.Y() >= bounds[0].Y() && player.X() <= bounds[1].X() && player.Y() <= bounds[1].Y() {
					player.ResetPath()
					player.AddState(world.MSBusy)
					go func() {
						defer func() {
							player.RemoveState(world.MSBusy)
						}()
						for _, fn := range world.InvOnObjectTriggers {
							if fn(player, object, invItem) {
								return
							}
						}
						player.SendPacket(world.DefaultActionMessage)
					}()
					return true
				}
				player.WalkTo(object.Location)
				return false
			}
			if player.AtObject(object) {
				player.ResetPath()
				player.AddState(world.MSBusy)
				go func() {
					defer func() {
						player.RemoveState(world.MSBusy)
					}()
					for _, fn := range world.InvOnObjectTriggers {
						if fn(player, object, invItem) {
							return
						}
					}
					player.SendPacket(world.DefaultActionMessage)
				}()
				return true
			}
			player.WalkTo(object.Location)
			return false
		})
	})
}