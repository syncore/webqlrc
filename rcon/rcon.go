package rcon

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
	"webqlrc/bridge"
	"webqlrc/config"

	zmq "github.com/pebbe/zmq4"
)

type qlSocketOrMsgType int

type message struct {
	incoming     chan string
	msgType      qlSocketOrMsgType
	timeReceived time.Time
}

type qlZmqSocket struct {
	address      string
	context      *zmq.Context
	socket       *zmq.Socket
	typeQlSocket qlSocketOrMsgType
}

const (
	smtRcon        qlSocketOrMsgType = 0
	smtMonitor     qlSocketOrMsgType = 1
	monitorAddress                   = "inproc://monitor-sock"
)

var cfg *config.Config
var socketMutex = &sync.Mutex{}

func createSockets() ([]*qlZmqSocket, error) {
	ctx, err := zmq.NewContext()
	if err != nil {
		return nil, fmt.Errorf("Context error: %s", err)
	}
	rconsocket, err := newQlZmqSocket(fmt.Sprintf("tcp://%s:%d", cfg.Rcon.QlZmqHost,
		cfg.Rcon.QlZmqRconPort), ctx, zmq.DEALER)

	if err != nil {
		return nil, err
	}
	monitorsocket, err := newQlZmqSocket(monitorAddress, ctx, zmq.PAIR)
	if err != nil {
		return nil, err
	}
	socks := []*qlZmqSocket{rconsocket, monitorsocket}
	err = rconsocket.socket.Monitor(monitorAddress, zmq.EVENT_ALL)
	if err != nil {
		return nil, fmt.Errorf("Monitor callback error: %s", err)
	}
	err = monitorsocket.socket.Connect(monitorAddress)
	if err != nil {
		return nil, fmt.Errorf("Unable to connect to monitor socket: %s", err)
	}
	err = rconsocket.openQlConnection(cfg.Rcon.QlZmqRconPassword)
	if err != nil {
		return nil, fmt.Errorf("Connection error: %s", err)
	}
	return socks, nil
}

func newQlZmqSocket(address string, context *zmq.Context,
	zmqSockType zmq.Type) (*qlZmqSocket, error) {

	s, err := zmq.NewSocket(zmqSockType)
	var qlstype qlSocketOrMsgType

	if zmqSockType == zmq.DEALER {
		qlstype = smtRcon
	} else if zmqSockType == zmq.PAIR {
		qlstype = smtMonitor
	}

	if err != nil {
		return nil, fmt.Errorf("Unable to create ZMQ socket of type %s",
			zmqSockType)
	}

	return &qlZmqSocket{
		address:      address,
		context:      context,
		socket:       s,
		typeQlSocket: qlstype,
	}, nil
}

func (rconsock *qlZmqSocket) openQlConnection(password string) error {
	rconsock.socket.SetPlainUsername("rcon")
	rconsock.socket.SetPlainPassword(password)
	rconsock.socket.SetZapDomain("rcon")
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	rconsock.socket.SetIdentity(fmt.Sprintf("i-%d", r.Int31n(2147483647)))
	fmt.Printf("Attempting to establish RCON connection to: %s\n", rconsock.address)
	err := rconsock.socket.Connect(rconsock.address)
	if err != nil {
		return fmt.Errorf("Unable to establish RCON connection: %s", err)
	}
	fmt.Printf("Registering connection to %s\n", rconsock.address)
	rconsock.socket.Send("register", 0)
	return nil
}

func (rconsock *qlZmqSocket) doRconAction(action string) {
	// ZMQ sockets are not thread-safe
	socketMutex.Lock()
	defer socketMutex.Unlock()
	rconsock.socket.Send(action, 0)
}

func readZmqSocketMsg(msg *message) {
	for m := range msg.incoming {
		if cfg.Rcon.QlZmqShowOnConsole {
			if msg.msgType == smtMonitor {
				fmt.Printf("[Monitor] %s\n", m)
			} else if msg.msgType == smtRcon {
				fmt.Printf("[Rcon] %s\n", m)
			}
		}
		// send to web ui
		bridge.MessageBridge.RconToWeb <- []byte(m)
	}
}

func startSocketMonitor(polltimeout time.Duration) {
	// Create sockets here so that polling will not need a lock
	qlzSockets, err := createSockets()
	if err != nil {
		log.Fatalf("FATAL: error when attempting to create sockets: %s", err)
	}
	// Incoming rcon messages from web
	for _, s := range qlzSockets {
		if s.typeQlSocket == smtRcon {
			go ListenForRconMessagesFromWeb(s)
		}
	}

	// Messages received from polled sockets to be read/processed
	socketMsg := &message{timeReceived: time.Now(), incoming: make(chan string)}
	go readZmqSocketMsg(socketMsg)

	// Sockets for zmq poller (*zmq4.Socket)
	var zRconSocket *zmq.Socket
	var zMonitorSocket *zmq.Socket
	for _, qzs := range qlzSockets {
		if qzs.typeQlSocket == smtRcon {
			zRconSocket = qzs.socket
		} else if qzs.typeQlSocket == smtMonitor {
			zMonitorSocket = qzs.socket
		}
	}

	poller := zmq.NewPoller()
	poller.Add(zRconSocket, zmq.POLLIN)
	poller.Add(zMonitorSocket, zmq.POLLIN)

	// Incoming messages from ZMQ
	for {
		zmqSockets, _ := poller.Poll(polltimeout)
		for _, zmqsock := range zmqSockets {
			switch z := zmqsock.Socket; z {
			case zRconSocket:
				msg, err := z.Recv(0)
				if err != nil {
					fmt.Printf("Error polling msg from rcon socket: %s\n", err)
					continue
				}
				if len(msg) != 0 {
					socketMsg.incoming <- msg
					socketMsg.msgType = smtRcon
					socketMsg.timeReceived = time.Now()
				}
			case zMonitorSocket:
				ev, adr, _, err := z.RecvEvent(0)
				if err != nil {
					fmt.Printf("Error polling msg from monitor socket: %s\n",
						err)
					continue
				}
				socketMsg.incoming <- fmt.Sprintf("%s %s", ev, adr)
				socketMsg.msgType = smtMonitor
				socketMsg.timeReceived = time.Now()
			}
		}
	}
}

// listen for messages from web ui to forward to rcon(zmq)
func ListenForRconMessagesFromWeb(rconsock *qlZmqSocket) {
	for m := range bridge.MessageBridge.OutToRcon {
		rconsock.doRconAction(string(m))
	}
}

func Start() {
	var err error
	cfg, err = config.ReadConfig(config.RCON)
	if err != nil {
		log.Fatalf("FATAL: unable to read RCON configuration file: %s", err)
	}

	go startSocketMonitor(cfg.Rcon.QlZmqRconPollTimeout * time.Millisecond)
	log.Printf("webqlrcon %s: Launched RCON interface\n", config.Version)
}
