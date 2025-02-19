package httptines

import (
	"slices"
	"sync"
	"time"
)

type target struct {
	address   string
	createdAt time.Time
}

type stat struct {
	targets    int
	successful []target
	waiting    []string
	m          sync.Mutex
}

func (s *stat) success(t string) {
	s.m.Lock()
	defer s.m.Unlock()
	s.successful = append(s.successful, target{address: t, createdAt: time.Now()})
}

func (s *stat) markWaiting(t string) {
	s.m.Lock()
	defer s.m.Unlock()
	if slices.Contains(s.waiting, t) {
		return
	}

	s.waiting = append(s.waiting, t)
}

func (s *stat) unmarkWaiting(t string) {
	s.m.Lock()
	defer s.m.Unlock()
	if i := slices.Index(s.waiting, t); i != -1 {
		s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
	}
}

func (s *stat) speed() int {
	rpm := 0
	lastMinute := time.Now().Add(-60 * time.Second)
	for i := len(s.successful) - 1; i > 0; i-- {
		if s.successful[i].createdAt.Compare(lastMinute) >= 0 {
			rpm++
		} else {
			break
		}
	}
	return rpm
}
