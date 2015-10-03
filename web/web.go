package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"os"
	"text/template"
	"time"
	"webqlrcon/bridge"
)

// intToDuration converts WebPongTimeout and WebSendTimeout to time.Duration
type webConfig struct {
	WebMaxMessageSize int64
	WebPongTimeout    int
	WebSendTimeout    int
	WebServerPort     int
}

type webSocketConn struct {
	w *websocket.Conn
}

const (
	webCfgFilename = "conf/web.conf"
)

var (
	rootTemplate = template.Must(template.ParseFiles("html/root_template.html"))
	upgrader     = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	cfg    *webConfig
	wsconn *webSocketConn
)

func readConfig(filename string) (wc *webConfig, err error) {
	f, openerr := os.Open(filename)
	defer f.Close()
	if openerr != nil {
		return nil, openerr
	}
	r := bufio.NewReader(f)
	dec := json.NewDecoder(r)
	err = dec.Decode(&wc)
	if err != nil {
		return nil, err
	}

	return wc, nil
}

func intToDuration(val int, dur time.Duration) time.Duration {
	return time.Duration(val) * dur
}

func (c *webSocketConn) readWebSocket() {
	defer c.w.Close()
	pongtimeout := intToDuration(cfg.WebPongTimeout, time.Second)
	c.w.SetReadLimit(cfg.WebMaxMessageSize)
	c.w.SetReadDeadline(time.Now().Add(pongtimeout))
	c.w.SetPongHandler(func(string) error {
		c.w.SetReadDeadline(time.Now().Add(pongtimeout))
		return nil
	})

	for {
		_, msg, err := c.w.ReadMessage()
		if err != nil {
			break
		}
		// Web UI (websocket) -> Rcon
		bridge.MessageBridge.WebToRcon <- msg
	}
}

func (c *webSocketConn) write(msgtype int, contents []byte) error {
	c.w.SetWriteDeadline(time.Now().Add(intToDuration(cfg.WebSendTimeout, time.Second)))
	return c.w.WriteMessage(msgtype, contents)
}

func (c *webSocketConn) writeWebSocket() {
	pingTicker := time.NewTicker(intToDuration((cfg.WebPongTimeout*9)/10, time.Second))
	defer func() {
		pingTicker.Stop()
		c.w.Close()
	}()
	for {
		select {
		// received msg from bridge (i.e. from rcon) that needs to go out to UI via websocket
		case msg, ok := <-bridge.MessageBridge.OutToWeb:
			if !ok {
				c.write(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.write(websocket.TextMessage, msg); err != nil {
				return
			}
		// ping
		case <-pingTicker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func serveRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "404: Not found", 404)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "405: Not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	rootTemplate.Execute(w, r.Host)
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "405: Not allowed", 405)
	}
	websock, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	wsconn = &webSocketConn{w: websock}
	go wsconn.writeWebSocket()
	wsconn.readWebSocket()
}

func StartWeb() {
	webconfig, err := readConfig(webCfgFilename)
	if err != nil {
		log.Fatalf("FATAL: unable to read web configuration file: %s", err)
	}
	cfg = webconfig
	http.HandleFunc("/", serveRoot)
	http.HandleFunc("/ws", serveWs)
	port := fmt.Sprintf(":%d", cfg.WebServerPort)
	log.Printf("webqlrcon: Starting web server on http://localhost%s", port)
	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("FATAL: unable to start webserver: %s", err)
	}
}
