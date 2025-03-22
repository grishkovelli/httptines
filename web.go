package httptines

import (
	"log"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"sync"
	"text/template"

	"github.com/gorilla/websocket"
)

// Global variables for web server management.
var (
	upgrader  = websocket.Upgrader{}           // WebSocket connection upgrader
	clients   = make(map[*websocket.Conn]bool) // Connected WebSocket clients
	broadcast = make(chan []byte)              // Channel for broadcasting messages
	wsm       sync.Mutex                       // Mutex for client map access
)

// Payload represents the structure of WebSocket messages.
type Payload struct {
	Kind string `json:"kind"` // Type of the message
	Body any    `json:"body"` // Content of the message
}

// listenAndServe starts the HTTP server on the specified port
// Parameters:
//   - port: Port number to listen on
func listenAndServe(port int) {
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/ws", wsHandler)

	fs := http.FileServer(http.Dir(absolutePath()))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	go handleMessages()

	log.Println("Server started on :", port)
	if err := http.ListenAndServe(":"+strconv.Itoa(port), nil); err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

// wsHandler handles incoming WebSocket connection requests
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}

	wsm.Lock()
	clients[conn] = true
	wsm.Unlock()
}

// handleMessages processes incoming messages from the broadcast channel.
func handleMessages() {
	for {
		msg := <-broadcast

		wsm.Lock()
		for c := range clients {
			err := c.WriteMessage(websocket.TextMessage, msg)
			if err != nil {
				c.Close()
				delete(clients, c)
			}
		}
		wsm.Unlock()
	}
}

// serveIndex serves the main HTML template page
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
func serveIndex(w http.ResponseWriter, r *http.Request) {
	t, err := template.ParseFiles(absolutePath() + "/template.html")
	if err != nil {
		panic(err)
	}

	if err = t.Execute(w, "ws://"+r.Host+"/ws"); err != nil {
		panic(err)
	}
}

// absolutePath returns the absolute path to the web directory
// Returns:
//   - string: Absolute path to web directory
func absolutePath() string {
	_, dir, _, _ := runtime.Caller(0)
	return path.Join(path.Dir(dir), "web")
}
