package httptines

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"slices"
	"sync"
	"time"

	"github.com/grishkovelli/httptines/pkg/wlpb"
)

//  ██╗    ██╗ ██████╗ ██████╗ ██╗  ██╗███████╗██████╗
//  ██║    ██║██╔═══██╗██╔══██╗██║ ██╔╝██╔════╝██╔══██╗
//  ██║ █╗ ██║██║   ██║██████╔╝█████╔╝ █████╗  ██████╔╝
//  ██║███╗██║██║   ██║██╔══██╗██╔═██╗ ██╔══╝  ██╔══██╗
//  ╚███╔███╔╝╚██████╔╝██║  ██║██║  ██╗███████╗██║  ██║
//   ╚══╝╚══╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
//

type Worker struct {
	Goroutines    int                 `default:"100"`
	ProxyTimeout  int                 `default:"10"`
	ProxyInterval int                 `default:"120"`
	Sources       map[string][]string `validate:"required"`
	Timeout       int                 `default:"10"`
	UserAgents    []string            `default:"Googlebot"`
	MinTimeSleep  int                 `default:"10"`
	MaxTimeSleep  int                 `default:"30"`
	WebPort       int                 `default:"8080"`

	b       *wlpb.Balancer
	m       sync.RWMutex
	stat    *Stat
	targets []string
}

func (w *Worker) Run(targets []string, testTarget string, handleBody func(io.Reader)) {
	validate(w)
	setDefaultValues(w)

	w.targets = targets
	w.b = &wlpb.Balancer{
		Requests:    w.Goroutines,
		Periodicity: w.ProxyInterval,
		Timeout:     w.ProxyTimeout,
		Sources:     w.Sources,
		TestURL:     testTarget,
	}
	w.stat = &Stat{Targets: len(w.targets), Balancer: w.b}

	queue := make(chan struct{}, w.Goroutines)

	go ListenAndServer(w.WebPort)
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

func (w *Worker) retrigger(u string) {
	w.m.Lock()
	w.targets = append(w.targets, u)
	w.m.Unlock()
}

func (w *Worker) shift(qty int) []string {
	w.m.Lock()
	defer w.m.Unlock()

	value := w.targets[:qty]
	w.targets = w.targets[qty:]
	return value
}

func (w *Worker) size() int {
	w.m.RLock()
	defer w.m.RUnlock()
	return len(w.targets)
}

func (w *Worker) userAgent() string {
	return w.UserAgents[rand.Intn(len(w.UserAgents))]
}

func (w *Worker) randTimeSleep() time.Duration {
	return time.Duration(rand.Intn(w.MaxTimeSleep-w.MinTimeSleep)+w.MinTimeSleep) * time.Millisecond
}

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

type Stat struct {
	Targets  int            `json:"targets"`
	Balancer *wlpb.Balancer `json:"balancer"`
	Waiting  int            `json:"waiting"`

	timestamps [][]time.Time
	waiting    []string
	m          sync.RWMutex
}

func (s *Stat) MarshalJSON() ([]byte, error) {
	type Alias Stat
	return json.Marshal(&struct {
		RPM       int `json:"RPM"`
		Processed int `json:"processed"`
		*Alias
	}{
		RPM:       s.computeRPM(),
		Processed: len(s.timestamps),
		Alias:     (*Alias)(s),
	})
}

func (s *Stat) addTimestamps(t1, t2 time.Time) {
	s.m.Lock()
	defer s.m.Unlock()
	s.timestamps = append(s.timestamps, []time.Time{t1, t2})
}

func (s *Stat) markWaiting(t string) {
	s.m.Lock()
	defer s.m.Unlock()
	if slices.Contains(s.waiting, t) {
		return
	}

	s.waiting = append(s.waiting, t)
	s.Waiting++
}

func (s *Stat) unmarkWaiting(t string) {
	s.m.Lock()
	defer s.m.Unlock()
	if i := slices.Index(s.waiting, t); i != -1 {
		s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
		s.Waiting--
	}
}

func (s *Stat) computeRPM() int {
	rpm := 0
	lastMinute := time.Now().Add(-60 * time.Second)
	for i := len(s.timestamps) - 1; i > 0; i-- {
		if s.timestamps[i][1].Compare(lastMinute) >= 0 {
			rpm++
		} else {
			break
		}
	}
	return rpm
}

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

func handleTarget(t string, w *Worker, queue <-chan struct{}, handleBody func(io.Reader)) {
	defer func() { <-queue }()

	var (
		rsp *wlpb.Response
		err error
		ok  bool
	)

	for {
		if rsp, err, ok = w.b.Request(t, w.userAgent()); ok {
			break
		} else {
			w.stat.markWaiting(t)
			time.Sleep(w.randTimeSleep())
		}
	}

	w.stat.unmarkWaiting(t)

	if err == nil {
		w.stat.addTimestamps(rsp.StartedAt, rsp.EndedAt)
		handleBody(rsp.Body)
	} else {
		w.retrigger(t)
	}
}

func writeLog(s string) {
	m := fmt.Sprintf("%s %s", time.Now().Format(time.DateTime), s)
	fmt.Println(m)
	p, _ := json.Marshal(Payload{"log", m})
	broadcast <- p
}
