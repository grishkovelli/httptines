package httptines

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grishkovelli/httptines/pkg/wlpb"
)

//  ██╗    ██╗ ██████╗ ██████╗ ██╗  ██╗███████╗██████╗
//  ██║    ██║██╔═══██╗██╔══██╗██║ ██╔╝██╔════╝██╔══██╗
//  ██║ █╗ ██║██║   ██║██████╔╝█████╔╝ █████╗  ██████╔╝
//  ██║███╗██║██║   ██║██╔══██╗██╔═██╗ ██╔══╝  ██╔══██╗
//  ╚███╔███╔╝╚██████╔╝██║  ██║██║  ██╗███████╗██║  ██║
//   ╚══╝╚══╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
//

// Worker represents a concurrent HTTP request processor that manages multiple goroutines
// to handle HTTP requests with rate limiting and load balancing capabilities.
type Worker struct {
	// Number of concurrent goroutines
	Goroutines int `default:"100"`

	// Time interval for rate limiting in seconds
	Interval int `default:"120"`

	// Map of source endpoints
	Sources map[string][]string `validate:"required"`

	// Request timeout in seconds
	Timeout int `default:"10"`

	// List of user agents to rotate through
	UserAgents []string `default:"Googlebot"`

	// Minimum sleep time between requests in milliseconds
	MinTimeSleep int `default:"10"`

	// Maximum sleep time between requests in milliseconds
	MaxTimeSleep int `default:"30"`

	// Port for the web interface
	WebPort int `default:"8080"`

	b       *wlpb.Balancer // Load balancer instance
	m       sync.RWMutex   // Mutex for thread-safe operations
	stat    *Stat          // Statistics tracker
	targets []string       // List of target URLs to process
}

// Run initializes and starts the worker with the given targets and handler function.
// Parameters:
//   - targets: List of URLs to process
//   - testTarget: URL used for testing the connection
//   - handleBody: Callback function to process the response body
func (w *Worker) Run(targets []string, testTarget string, handleBody func([]byte)) {
	validate(w)
	setDefaultValues(w)

	w.targets = targets
	w.b = &wlpb.Balancer{
		Requests:    w.Goroutines,
		Periodicity: w.Interval,
		Timeout:     w.Timeout,
		Sources:     w.Sources,
		TestURL:     testTarget,
	}
	w.stat = &Stat{Targets: len(w.targets), Balancer: w.b}

	queue := make(chan struct{}, w.Goroutines)

	go listenAndServer(w.WebPort)
	go w.b.Run(writeLog)
	go w.sendStat()

	for {
		size := w.size()

		if size < 1 {
			fmt.Println("No items")
			break
		}

		qty := int(math.Min(float64(w.Goroutines), float64(size)))
		targets := w.shift(qty)

		for _, t := range targets {
			queue <- struct{}{}
			go handleTarget(t, w, queue, handleBody)
		}
	}
}

// retrigger adds a URL back to the target list for reprocessing.
// Parameters:
//   - u: URL to be reprocessed
func (w *Worker) retrigger(u string) {
	w.m.Lock()
	w.targets = append(w.targets, u)
	w.m.Unlock()
}

// shift removes and returns the first n targets from the worker's target list.
// Parameters:
//   - n: Number of targets to remove and return
//
// Returns:
//   - []string: Slice of removed targets
func (w *Worker) shift(n int) []string {
	w.m.Lock()
	defer w.m.Unlock()

	value := w.targets[:n]
	w.targets = w.targets[n:]
	return value
}

// size returns the current number of remaining targets.
// Returns:
//   - int: Number of remaining targets
func (w *Worker) size() int {
	w.m.RLock()
	defer w.m.RUnlock()
	return len(w.targets)
}

// userAgent returns a randomly selected user agent from the configured list.
// Returns:
//   - string: Selected user agent string
func (w *Worker) userAgent() string {
	return w.UserAgents[rand.Intn(len(w.UserAgents))]
}

// randTimeSleep returns a random duration between MinTimeSleep and MaxTimeSleep.
// Returns:
//   - time.Duration: Random sleep duration
func (w *Worker) randTimeSleep() time.Duration {
	return time.Duration(rand.Intn(w.MaxTimeSleep-w.MinTimeSleep)+w.MinTimeSleep) * time.Millisecond
}

// sendStat periodically sends statistics about the worker's state to connected clients through the web interface.
func (w *Worker) sendStat() {
	for {
		w.stat.m.RLock()
		p, _ := json.Marshal(Payload{"stat", w.stat})
		w.stat.m.RUnlock()
		broadcast <- p
		time.Sleep(time.Second * 3)
	}
}

//  ███████╗████████╗ █████╗ ████████╗
//  ██╔════╝╚══██╔══╝██╔══██╗╚══██╔══╝
//  ███████╗   ██║   ███████║   ██║
//  ╚════██║   ██║   ██╔══██║   ██║
//  ███████║   ██║   ██║  ██║   ██║
//  ╚══════╝   ╚═╝   ╚═╝  ╚═╝   ╚═╝
//

// Stat tracks statistics about the worker's operations and target processing state.
type Stat struct {
	// Total number of targets
	Targets int `json:"targets"`

	// Reference to the load balancer
	Balancer *wlpb.Balancer `json:"balancer"`

	// Number of targets waiting to be processed
	Waiting int `json:"waiting"`

	waiting []string     // List of targets currently waiting
	m       sync.RWMutex // Mutex for thread-safe operations
}

// markWaiting marks a target as waiting for processing.
// Parameters:
//   - t: Target URL to mark as waiting
func (s *Stat) markWaiting(t string) {
	s.m.Lock()
	defer s.m.Unlock()
	if slices.Contains(s.waiting, t) {
		return
	}

	s.waiting = append(s.waiting, t)
	s.Waiting++
}

// unmarkWaiting removes a target from the waiting list.
// Parameters:
//   - t: Target URL to remove from waiting list
func (s *Stat) unmarkWaiting(t string) {
	s.m.Lock()
	if i := slices.Index(s.waiting, t); i != -1 {
		s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
		s.Waiting--
	}
	s.m.Unlock()
}

//  ██╗    ██╗███████╗██████╗
//  ██║    ██║██╔════╝██╔══██╗
//  ██║ █╗ ██║█████╗  ██████╔╝
//  ██║███╗██║██╔══╝  ██╔══██╗
//  ╚███╔███╔╝███████╗██████╔╝
//   ╚══╝╚══╝ ╚══════╝╚═════╝
//

// upgrader is used to upgrade HTTP connections to WebSocket connections
var upgrader = websocket.Upgrader{}

// clients maintains a map of all connected WebSocket clients
// The boolean value is used as a set-like structure (the value is always true)
var clients = make(map[*websocket.Conn]bool)

// broadcast is a channel used to send messages to all connected clients
var broadcast = make(chan []byte)

// wsm is a mutex to protect concurrent access to the clients map
var wsm sync.Mutex

// Payload represents the structure of WebSocket messages
type Payload struct {
	Kind string `json:"kind"` // Type of the message
	Body any    `json:"body"` // Content of the message, can be of any type
}

// listenAndServer starts the HTTP server on the specified port
// It sets up routes for the index page, WebSocket connections, and static files
// The server runs indefinitely until an error occurs
func listenAndServer(port int) {
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
// It upgrades HTTP connections to WebSocket connections and registers them in the clients map
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

// handleMessages processes incoming messages from the broadcast channel
// It runs in a separate goroutine and sends messages to all connected clients
// If a client connection errors out, it is removed from the clients map
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
// It parses the template file and injects the WebSocket connection URL
func serveIndex(w http.ResponseWriter, r *http.Request) {
	t, err := template.ParseFiles(absolutePath() + "/template.html")
	if err != nil {
		panic(err)
	}

	t.Execute(w, "ws://"+r.Host+"/ws")
}

// absolutePath returns the absolute path to the web directory
// It uses runtime.Caller to determine the current file's location
// and constructs the path to the web directory relative to it
func absolutePath() string {
	_, dir, _, _ := runtime.Caller(0)
	return path.Join(path.Dir(dir), "web")
}

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

// handleTarget processes a single target URL with retry capability.
// Parameters:
//   - t: Target URL to process
//   - w: Worker instance
//   - queue: Channel for managing concurrent goroutines
//   - handleBody: Callback function to process the response body
func handleTarget(t string, w *Worker, queue <-chan struct{}, handleBody func([]byte)) {
	defer func() { <-queue }()

	var (
		body []byte
		err  error
		ok   bool
	)

	for {
		if body, err, ok = w.b.Request(t, w.userAgent()); ok {
			break
		} else {
			w.stat.markWaiting(t)
			time.Sleep(w.randTimeSleep())
		}
	}

	w.stat.unmarkWaiting(t)

	if err == nil {
		handleBody(body)
	} else {
		w.retrigger(t)
	}
}

// writeLog writes a log message to stdout and broadcasts it to connected clients.
// Parameters:
//   - s: Log message to write
func writeLog(s string) {
	m := fmt.Sprintf("%s %s", time.Now().Format(time.DateTime), s)
	fmt.Println(m)
	p, _ := json.Marshal(Payload{"log", m})
	broadcast <- p
}

// setDefaultValues sets default values for struct fields based on their "default" tags.
// Parameters:
//   - obj: Pointer to the struct to initialize
func setDefaultValues(obj interface{}) {
	tof := reflect.TypeOf(obj).Elem()
	vof := reflect.ValueOf(obj).Elem()

	for i := 0; i < vof.NumField(); i++ {
		vf := vof.Field(i)
		v := tof.Field(i).Tag.Get("default")

		if v == "" || !vf.IsZero() {
			continue
		}

		switch vf.Kind() {
		case reflect.String:
			vf.SetString(v)
		case reflect.Int:
			if intv, err := strconv.ParseInt(v, 10, 64); err == nil {
				vf.SetInt(intv)
			}
		case reflect.Slice:
			if vf.Type().Elem().Kind() == reflect.String {
				values := strings.Split(v, ",")
				vf.Set(reflect.ValueOf(values))
			}
		}
	}
}

// validate checks if required fields in a struct are set based on their "validate" tags.
// Parameters:
//   - obj: Pointer to the struct to validate
func validate(obj interface{}) {
	tof := reflect.TypeOf(obj).Elem()
	vof := reflect.ValueOf(obj).Elem()

	for i := 0; i < vof.NumField(); i++ {
		tf := tof.Field(i)
		vf := vof.Field(i)

		v := tf.Tag.Get("validate")
		if v == "" {
			continue
		}

		if strings.Contains(v, "required") && vf.IsZero() {
			fmt.Printf("Field \"%s\" is required\n", tf.Name)
			os.Exit(0)
		}
	}
}
