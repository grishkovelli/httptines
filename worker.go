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
	Goroutines    int                 `default:"100"`
	ProxyTimeout  int                 `default:"10"`
	ProxyInterval int                 `default:"120"`
	Sources       map[string][]string `validate:"required"`
	Timeout       int                 `default:"10"`
	UserAgents    []string            `default:"Googlebot"`
	MinTimeSleep  int                 `default:"10"`
	MaxTimeSleep  int                 `default:"30"`

	b          *balancer
	m          sync.RWMutex
	stat       *stat
	targets    []string
	testTarget string
}

func (w *Worker) Run(targets []string, testTarget string, handleBody func(io.Reader)) {
	validate(w)
	setDefaultValues(w)

	w.targets = targets
	w.testTarget = testTarget
	w.b = &balancer{w: w}
	w.stat = &stat{targets: len(w.targets)}

	queue := make(chan struct{}, w.Goroutines)
	runup := make(chan struct{}, 1)

	go w.b.run(runup)
	go func() {
		for {
			w.printStat()
			time.Sleep(time.Second * 3)
		}
	}()

	<-runup

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

func (w *Worker) printStat() {
	w.stat.m.Lock()
	w.b.m.Lock()
	defer w.stat.m.Unlock()
	defer w.b.m.Unlock()
	fmt.Printf(
		"\ntargets=%d/%d rpm=%d waiting=%d\n",
		len(w.stat.successful),
		w.stat.targets,
		w.stat.speed(),
		len(w.stat.waiting),
	)
	for _, s := range w.b.alive {
		fmt.Println(s)
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

	startAt := time.Now()

	for {
		if s = w.b.nextServer(); s == nil {
			w.stat.markWaiting(t)
			time.Sleep(w.randTimeSleep())
		} else {
			break
		}
	}
	defer s.registerFinish(startAt)

	w.stat.unmarkWaiting(t)

	body, err := doRequest(t, s, w.Timeout, w.userAgent())
	if err != nil {
		w.retrigger(t)
		w.b.analyzeServer(s)
	} else {
		w.stat.success(t)
		s.m.Lock()
		s.positive++
		s.m.Unlock()
		handleBody(body)
	}
}
