package rcon

import (
	"bufio"
	"encoding/json"
	"fmt"
	zmq "github.com/pebbe/zmq4"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"
	"webqlrcon/bridge"
)

type qlZmqConfig struct {
	// for JSON
	QlZmqHost            string
	QlZmqRconPort        int
	QlZmqRconPassword    string
	QlZmqRconPollTimeOut time.Duration
}

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
	qlZmqCfgFilename                   = "conf/rcon.conf"
	smtRcon          qlSocketOrMsgType = 0
	smtMonitor       qlSocketOrMsgType = 1
	monitorAddress                     = "inproc://monitor-sock"
)

var cfg *qlZmqConfig
var socketMutex = &sync.Mutex{}

func newQlZmqSocket(address string, context *zmq.Context, zmqSockType zmq.Type) (*qlZmqSocket, error) {
	s, err := zmq.NewSocket(zmqSockType)
	var qlstype qlSocketOrMsgType
	if zmqSockType == zmq.DEALER {
		qlstype = smtRcon
	} else if zmqSockType == zmq.PAIR {
		qlstype = smtMonitor
	}
	if err != nil {
		return nil, fmt.Errorf("Unable to create ZMQ socket of type %s", zmqSockType)
	}
	return &qlZmqSocket{
		address:      address,
		context:      context,
		socket:       s,
		typeQlSocket: qlstype,
	}, nil
}

func createSockets() ([]*qlZmqSocket, error) {
	ctx, err := zmq.NewContext()
	if err != nil {
		return nil, fmt.Errorf("Context error: %s", err)
	}
	rconsocket, err := newQlZmqSocket(fmt.Sprintf("tcp://%s:%d", cfg.QlZmqHost, cfg.QlZmqRconPort), ctx, zmq.DEALER)
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
		return nil, fmt.Errorf("Monitor callback error: %s, err")
	}
	err = monitorsocket.socket.Connect(monitorAddress)
	if err != nil {
		return nil, fmt.Errorf("Unable to connect to monitor socket: %s", err)
	}
	err = openQlConnection(rconsocket, cfg.QlZmqRconPassword)
	if err != nil {
		return nil, fmt.Errorf("Connection error: %s", err)
	}
	return socks, nil
}

func (rconsock *qlZmqSocket) doRconAction(action string) {
	// ZMQ sockets are not thread-safe
	socketMutex.Lock()
	defer socketMutex.Unlock()
	rconsock.socket.Send(action, 0)
}

func openQlConnection(s *qlZmqSocket, password string) error {
	s.socket.SetPlainUsername("rcon")
	s.socket.SetPlainPassword(password)
	s.socket.SetZapDomain("rcon")
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	s.socket.SetIdentity(fmt.Sprintf("i-%d", r.Int31n(2147483647)))
	fmt.Printf("Attempting to establish RCON connection to: %s\n", s.address)
	err := s.socket.Connect(s.address)
	if err != nil {
		return fmt.Errorf("Unable to establish RCON connection: %s", err)
	}
	fmt.Printf("Registering connection to %s\n", s.address)
	s.socket.Send("register", 0)
	return nil
}

func readZmqSocketMsg(msg *message) {
	for m := range msg.incoming {
		// Eventually printing wont be used, since everything is shown in web
		if msg.msgType == smtMonitor {
			fmt.Printf("[Monitor] %s\n", m)
		} else if msg.msgType == smtRcon {
			fmt.Printf("[Rcon] %s\n", m)
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
					fmt.Printf("Error polling msg from monitor socket: %s\n", err)
					continue
				}
				socketMsg.incoming <- fmt.Sprintf("%s %s", ev, adr)
				socketMsg.msgType = smtMonitor
				socketMsg.timeReceived = time.Now()
			}
		}
	}
}

func readConfig(filename string) (qc *qlZmqConfig, err error) {
	f, err := os.Open(filename)
	defer f.Close()
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(f)
	dec := json.NewDecoder(r)
	err = dec.Decode(&qc)
	if err != nil {
		return nil, err
	}

	return qc, nil
}

// listen for messages from web ui to forward to rcon(zmq)
func ListenForRconMessagesFromWeb(rconsock *qlZmqSocket) {
	for m := range bridge.MessageBridge.OutToRcon {
		rconsock.doRconAction(string(m))
	}
}

func StartRcon() {
	rconconfig, err := readConfig(qlZmqCfgFilename)
	if err != nil {
		log.Fatalf("FATAL: unable to read rcon configuration file: %s", err)
	}
	cfg = rconconfig

	go startSocketMonitor(cfg.QlZmqRconPollTimeOut * time.Millisecond)
	log.Println("webqlrcon: Launched RCON interface")
}
