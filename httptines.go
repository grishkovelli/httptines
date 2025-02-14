package httptines

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"sync"
	"time"
)

//  ██╗    ██╗ ██████╗ ██████╗ ██╗  ██╗███████╗██████╗
//  ██║    ██║██╔═══██╗██╔══██╗██║ ██╔╝██╔════╝██╔══██╗
//  ██║ █╗ ██║██║   ██║██████╔╝█████╔╝ █████╗  ██████╔╝
//  ██║███╗██║██║   ██║██╔══██╗██╔═██╗ ██╔══╝  ██╔══██╗
//  ╚███╔███╔╝╚██████╔╝██║  ██║██║  ██╗███████╗██║  ██║
//   ╚══╝╚══╝  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
//

type Worker struct {
	Parallel      int                 `default:"100"`
	ProxyTimeout  int                 `default:"10"`
	ProxyInterval int                 `default:"120"`
	Sources       map[string][]string `validate:"required"`
	Targets       []string            `validate:"required"`
	TestTarget    string              `validate:"required"`
	Timeout       int                 `default:"10"`
	UserAgents    []string            `default:"Googlebot"`
	MinTimeSleep  int                 `default:"10"`
	MaxTimeSleep  int                 `default:"30"`

	bl *balancer
	mu sync.RWMutex
}

func (w *Worker) retrigger(u string) {
	w.mu.Lock()
	w.Targets = append(w.Targets, u)
	w.mu.Unlock()
}

func (w *Worker) shift(qty int) []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	value := w.Targets[:qty]
	w.Targets = w.Targets[qty:]
	return value
}

func (w *Worker) size() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.Targets)
}

func (w *Worker) userAgent() string {
	return w.UserAgents[rand.Intn(len(w.UserAgents))]
}

func (w *Worker) randTimeSleep() time.Duration {
	return time.Duration(rand.Intn(w.MaxTimeSleep-w.MinTimeSleep)+w.MinTimeSleep) * time.Millisecond
}

func (w *Worker) Run(handleBody func(io.Reader)) {
	validate(w)
	setDefaultValues(w)

	w.bl = &balancer{w: w}

	queue := make(chan struct{}, w.Parallel)
	runup := make(chan struct{}, 1)

	go w.bl.run(runup)

	<-runup

	for {
		size := w.size()

		if size < 1 {
			fmt.Println("No items")
			break
		}

		qty := int(math.Min(float64(w.Parallel), float64(size)))
		targets := w.shift(qty)

		for _, t := range targets {
			queue <- struct{}{}
			go handleTarget(t, w, queue, handleBody)
		}
	}
}

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

func handleTarget(t string, w *Worker, ch <-chan struct{}, handleBody func(io.Reader)) {
	defer func() { <-ch }()
	var s *server

	for {
		if s = w.bl.nextServer(); s == nil {
			time.Sleep(w.randTimeSleep())
		} else {
			break
		}
	}

	body, err := doRequest(t, s, w.Timeout, w.userAgent())
	if err != nil {
		w.retrigger(t)
		w.bl.removeServer(s)
	} else {
		handleBody(body)
	}
}
