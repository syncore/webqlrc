// bridge.go: Bridge for rcon (zmq) sockets <-> websocket
package bridge

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
		case trmsg := <-b.WebToRcon:
			b.OutToRcon <- trmsg
		}
	}
}
