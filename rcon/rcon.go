package rcon

import (
	"bufio"
	"encoding/json"
	"fmt"
	zmq "github.com/pebbe/zmq4"
	"math/rand"
	"os"
	"qlrcon/bridge"
	"sync"
	"time"
)

type qlZmqConfig struct {
	QlZmqHost            string
	QlZmqRconPort        int
	QlZmqRconPassword    string
	QlZmqRconPollTimeOut time.Duration
}

type message struct {
	content      []byte
	incoming     chan string
	timeReceived time.Time
}

type qlSocketType int

const (
	qlZmqCfgFilename              = "conf/rcon.conf"
	socketRcon       qlSocketType = 0
	socketMonitor    qlSocketType = 1
	monitorAddress                = "inproc://monitor-sock"
)

var cfg *qlZmqConfig

type qlZmqSocket struct {
	address      string
	context      *zmq.Context
	mut          sync.Mutex
	socket       *zmq.Socket
	typeQlSocket qlSocketType
}

func newQlZmqSocket(address string, context *zmq.Context, socktype zmq.Type) (*qlZmqSocket, error) {
	s, err := zmq.NewSocket(socktype)
	var qlstype qlSocketType
	if socktype == zmq.DEALER {
		qlstype = socketRcon
	} else if socktype == zmq.PAIR {
		qlstype = socketMonitor
	}
	if err != nil {
		return nil, fmt.Errorf("Unable to create ZMQ socket of type %s", socktype)
	}
	return &qlZmqSocket{
		address:      address,
		context:      context,
		socket:       s,
		typeQlSocket: qlstype,
	}, nil
}

func initSockets() ([]*qlZmqSocket, error) {
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

//func processRconMessage(incoming <-chan string, rconsock *QlZmqSocket) {
//	for msg := range incoming {
//		fmt.Printf("Got msg in processRconMessage: %s\n", msg)
//		if strings.Contains(msg, "x0x0") {
//			rconsock.doRconAction("say hello")
//		}
//		if strings.Contains(msg, "b0b0") {
//			rconsock.doRconAction("say goodbye")
//		}
//	}
//}

func (rconsock *qlZmqSocket) doRconAction(action string) {
	// ZMQ sockets are not thread-safe
	rconsock.mut.Lock()
	defer rconsock.mut.Unlock()
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

func readZmqMonitorSocketMsg(msg *message) {
	// Right now we're only we're only reading from the channel
	for m := range msg.incoming {
		fmt.Printf("Monitor socket: %s\n", m)
		// send to web ui
		bridge.MessageBridge.RconToWeb <- []byte(m)
	}
}

func readZmqRconSocketMsg(msg *message, rconsock *qlZmqSocket) {
	//go processRconMessage(msg.Outgoing, rconsock)
	for m := range msg.incoming {
		fmt.Printf("Rcon socket: %s\n", m)

		// send to web ui
		bridge.MessageBridge.RconToWeb <- []byte(m)

		//		if strings.Contains(m, "hey") {
		//			msg.Outgoing <- "x0x0"
		//		}
		//		if strings.Contains(m, "byebye") {
		//			msg.Outgoing <- "b0b0"
		//		}
	}
}

func monitorSockets(qlzsockets []*qlZmqSocket, polltimeout time.Duration) {
	// Message received from polled RCON socket to be read/processed
	rconMsg := &message{
		timeReceived: time.Now(),
		incoming:     make(chan string),
	}
	// Message received from polled monitor socket to be read/processed
	monMsg := &message{
		timeReceived: time.Now(),
		incoming:     make(chan string),
	}

	// Sockets (*zmq4.Socket)
	var zRconSocket *zmq.Socket
	var zMonitorSocket *zmq.Socket

	// *qlZmqSocket
	var qRconSocket *qlZmqSocket

	for _, qzs := range qlzsockets {
		if qzs.typeQlSocket == socketRcon {
			qRconSocket = qzs
			zRconSocket = qzs.socket
		} else if qzs.typeQlSocket == socketMonitor {
			zMonitorSocket = qzs.socket
		}
	}

	go readZmqMonitorSocketMsg(monMsg)
	go readZmqRconSocketMsg(rconMsg, qRconSocket)

	poller := zmq.NewPoller()
	poller.Add(zRconSocket, zmq.POLLIN)
	poller.Add(zMonitorSocket, zmq.POLLIN)

	for {
		zmqsockets, _ := poller.Poll(polltimeout)
		for _, zmqsock := range zmqsockets {
			switch z := zmqsock.Socket; z {
			case zRconSocket:
				// Process Rcon socket message
				// see note in StartRcon(); z.Recv() here might require a lock
				msg, err := z.Recv(0)
				if err != nil {
					fmt.Printf("Error polling msg from rcon socket: %s\n", err)
					continue
				}
				if len(msg) != 0 {
					rconMsg.incoming <- msg
					rconMsg.timeReceived = time.Now()
				}
			case zMonitorSocket:
				// Process Monitor socket messages
				// see note in StartRcon(); z.RecvEvent() here might require a lock
				ev, adr, val, err := z.RecvEvent(0)
				if err != nil {
					fmt.Printf("Error polling msg from monitor socket: %s\n", err)
					continue
				}
				monMsg.incoming <- fmt.Sprintf("%s %s %d", ev, adr, val)
				monMsg.timeReceived = time.Now()
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

// listen for messages from web ui to send to rcon(zmq)
func ListenForMessagesFromWeb(rconsock *qlZmqSocket) {
	for m := range bridge.MessageBridge.OutToRcon {
		// received message from web UI, send it to socket
		rconsock.doRconAction(string(m))
	}
}

func StartRcon() error {
	rconconfig, err := readConfig(qlZmqCfgFilename)
	if err != nil {
		return fmt.Errorf("Unable to read rcon configuration file: %s", err)
	}
	cfg = &qlZmqConfig{
		QlZmqHost:            rconconfig.QlZmqHost,
		QlZmqRconPort:        rconconfig.QlZmqRconPort,
		QlZmqRconPassword:    rconconfig.QlZmqRconPassword,
		QlZmqRconPollTimeOut: rconconfig.QlZmqRconPollTimeOut,
	}
	sockets, err := initSockets()
	if err != nil {
		return fmt.Errorf("Error when attempting to create sockets: %s", err)
	}

	for _, s := range sockets {
		if s.typeQlSocket == socketRcon {
			go ListenForMessagesFromWeb(s)
		}
	}

	// passing sockets as part of this goroutine might not be thread-safe; investigate
	go monitorSockets(sockets, cfg.QlZmqRconPollTimeOut*time.Millisecond)
	fmt.Println("qlrcon: Launched RCON interface")
	return nil
}
