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

var upgrader = websocket.Upgrader{}
var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan []byte)
var wsm sync.Mutex

type Payload struct {
	Kind string `json:"kind"`
	Body any    `json:"body"`
}

func ListenAndServer(port int) {
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

func serveIndex(w http.ResponseWriter, r *http.Request) {
	t, err := template.ParseFiles(absolutePath() + "/template.html")
	if err != nil {
		panic(err)
	}

	t.Execute(w, "ws://"+r.Host+"/ws")
}

func absolutePath() string {
	_, dir, _, _ := runtime.Caller(0)
	return path.Join(path.Dir(dir), "web")
}
