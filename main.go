package main

import (
	"log"
	"webqlrcon/bridge"
	"webqlrcon/rcon"
	"webqlrcon/web"
)

const version = "0.1"

func main() {
	go bridge.MessageBridge.PassMessages()
	log.Printf("Starting webqlrcon v%s", version)
	rcon.StartRcon()
	web.StartWeb()
}
