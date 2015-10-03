// Bridge: rcon (zmq) sockets <-> websocket
package bridge

//import (
//	"fmt"
//)

type bridge struct {
	RconToWeb chan []byte
	WebToRcon chan []byte
	OutToWeb  chan []byte
	OutToRcon chan []byte
}

var MessageBridge = &bridge{
	RconToWeb: make(chan []byte),
	WebToRcon: make(chan []byte),
	OutToRcon: make(chan []byte),
	OutToWeb:  make(chan []byte),
}

func (b *bridge) PassMessages() {
	for {
		select {
		case twmsg := <-b.RconToWeb:
			b.OutToWeb <- twmsg
			//fmt.Printf("Got: %s from rcon(zmq) forwarding to web", string(twmsg))
		case trmsg := <-b.WebToRcon:
			b.OutToRcon <- trmsg
			//fmt.Printf("Got: %s from web(websocket) forwarding to rcon", string(trmsg))
			//default:
			//fmt.Println("MessageBridge: doing nothing")
		}
	}
}
