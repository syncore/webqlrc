// web.go - Web server and websocket operations.
package web

import (
	"fmt"
	"log"
	"net/http"
	"path"
	"text/template"
	"time"
	"webqlrc/bridge"
	"webqlrc/config"

	"github.com/apexskier/httpauth"
	"github.com/gorilla/websocket"
)

type webSocketConn struct {
	w *websocket.Conn
}

const (
	mainRoute      = "/"
	getLoginRoute  = "/login"
	postLoginRoute = "/sendlogin"
	webSocketRoute = "/ws"
)

var (
	cfg           *config.Config
	loginTemplate = template.Must(template.ParseFiles("html/login_template.html"))
	rootTemplate  = template.Must(template.ParseFiles("html/root_template.html"))
	upgrader      = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
	webauthbackend httpauth.GobFileAuthBackend
	webauthorizer  httpauth.Authorizer
	webroles       = config.WebRoles
	wsconn         *webSocketConn
)

func intToDuration(val int, dur time.Duration) time.Duration {
	return time.Duration(val) * dur
}

func (c *webSocketConn) readWebSocket() {
	defer c.w.Close()
	pongtimeout := intToDuration(cfg.Web.WebPongTimeout, time.Second)
	c.w.SetReadLimit(cfg.Web.WebMaxMessageSize)
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
	c.w.SetWriteDeadline(time.Now().Add(intToDuration(cfg.Web.WebSendTimeout,
		time.Second)))
	return c.w.WriteMessage(msgtype, contents)
}

func (c *webSocketConn) writeWebSocket() {
	pingTicker := time.NewTicker(intToDuration((cfg.Web.WebPongTimeout*9)/10,
		time.Second))
	defer func() {
		pingTicker.Stop()
		c.w.Close()
	}()
	for {
		select {
		// recv msg from bridge (i.e. from rcon) that needs to go out to UI via websocket
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

func serveGetLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "405: Not allowed", 405)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Messages       []string
		PostLoginRoute string
	}{
		webauthorizer.Messages(w, r),
		postLoginRoute,
	}
	loginTemplate.Execute(w, data)
}

func servePostLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "405: Not allowed", 405)
		return
	}
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	if err := webauthorizer.Login(w, r, username, password,
		mainRoute); err != nil && err.Error() == "already authenticated" {
		http.Redirect(w, r, mainRoute, http.StatusSeeOther)
	} else if err != nil {
		http.Redirect(w, r, getLoginRoute, http.StatusSeeOther)
	}
}

func serveRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != mainRoute {
		http.Error(w, "404: Not found", 404)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "405: Not allowed", 405)
		return
	}
	if err := webauthorizer.Authorize(w, r, true); err != nil {
		http.Redirect(w, r, getLoginRoute, http.StatusSeeOther)
		return
	}
	if user, err := webauthorizer.CurrentUser(w, r); err == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data := struct {
			User httpauth.UserData
			Host string
		}{
			user,
			r.Host,
		}
		rootTemplate.Execute(w, data)
	}
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

func Start() {
	var err error
	cfg, err = config.ReadConfig(config.WEB)
	if err != nil {
		log.Fatalf("FATAL: unable to read web configuration file: %s", err)
	}
	port := fmt.Sprintf(":%d", cfg.Web.WebServerPort)
	log.Printf("webqlrcon %s: Starting web server on http://localhost%s",
		config.Version, port)

	webauthbackend, err := httpauth.NewGobFileAuthBackend(path.Join(config.ConfigurationDirectory,
		config.WebUserFilename))
	if err != nil {
		log.Fatalf("FATAL: unable to create web authorization backend: %s", err)
	}

	webauthorizer, err = httpauth.NewAuthorizer(webauthbackend,
		[]byte("cookie-encryption-key"), "admin", webroles)
	if err != nil {
		log.Fatalf("FATAL: unable to create web authorizer: %s", err)
	}

	http.HandleFunc(mainRoute, serveRoot)
	http.HandleFunc(getLoginRoute, serveGetLogin)
	http.HandleFunc(postLoginRoute, servePostLogin)
	http.HandleFunc(webSocketRoute, serveWs)
	err = http.ListenAndServe(port, nil)
	if err != nil {
		log.Fatalf("FATAL: unable to start webserver: %s", err)
	}
}
