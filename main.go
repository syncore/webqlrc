package main

import (
	"log"
	"qlrcon/bridge"
	"qlrcon/rcon"
	"qlrcon/web"
)

func main() {
	go bridge.MessageBridge.PassMessages()

	if err := rcon.StartRcon(); err != nil {
		log.Fatalf("FATAL: error when starting rcon: %s", err)
	}

	web.StartWeb()
}
