package wlpb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var toggleSortProxies int32
var writeLogFunc func(string)

//  ██████╗  █████╗ ██╗      █████╗ ███╗   ██╗ ██████╗███████╗██████╗
//  ██╔══██╗██╔══██╗██║     ██╔══██╗████╗  ██║██╔════╝██╔════╝██╔══██╗
//  ██████╔╝███████║██║     ███████║██╔██╗ ██║██║     █████╗  ██████╔╝
//  ██╔══██╗██╔══██║██║     ██╔══██║██║╚██╗██║██║     ██╔══╝  ██╔══██╗
//  ██████╔╝██║  ██║███████╗██║  ██║██║ ╚████║╚██████╗███████╗██║  ██║
//  ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝  ╚═══╝ ╚═════╝╚══════╝╚═╝  ╚═╝
//

type Balancer struct {
	Requests    int                 `json:"requests"`
	Periodicity int                 `json:"periodicity"`
	Sources     map[string][]string `json:"sources"`
	TestURL     string              `json:"testURL"`
	Timeout     int                 `json:"timeout"`
	UserAgent   string              `json:"userAgent"`

	alive   []*Server
	proxies []*url.URL
	m       sync.RWMutex
}

func (b *Balancer) Analyze(s *Server) {
	b.m.Lock()
	defer b.m.Unlock()

	s.m.Lock()
	defer s.m.Unlock()

	s.Negative++
	i := slices.IndexFunc(b.alive, func(i *Server) bool {
		return i.URL == s.URL
	})

	mult3x := (s.Positive == 0 && s.Negative >= 3) || (s.Positive > 0 && (s.Negative/s.Positive) >= 3)
	if i >= 0 && mult3x {
		b.alive = append(b.alive[:i], b.alive[i+1:]...)
		return
	}

	switch {
	case s.Limit > 0:
		s.Limit--
	case s.Requests > 0:
		s.Limit = s.Requests - 1
	default:
		s.Limit = 0
	}
}

func (b *Balancer) Run(writeLog func(string)) {
	writeLogFunc = writeLog

	ticker := time.NewTicker(time.Duration(b.Periodicity) * time.Second)
	defer ticker.Stop()

	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					writeLogFunc(fmt.Sprintf("recovered: %v", r))
				}
			}()
			b.fetchProxies()
			b.checkProxies()
		}()
		<-ticker.C
	}
}

func (b *Balancer) NextServer() *Server {
	b.m.Lock()
	defer b.m.Unlock()

	for _, s := range b.alive {
		s.m.Lock()
	}

	computeCapacity(b.Requests, b.alive)
	sortAliveProxies(b.alive)
	bs := bestServer(b.alive)

	for _, s := range b.alive {
		s.m.Unlock()
	}

	return bs
}

func (b *Balancer) Request(target, agent string) (*Response, error, bool) {
	s := b.NextServer()
	if s == nil {
		return nil, nil, false
	}

	r, err := makeRequest(target, agent, b.Timeout, s)
	if err != nil {
		b.Analyze(s)
		return r, err, true
	} else {
		s.PositiveUp()
	}
	return r, err, true
}

func (b *Balancer) MarshalJSON() ([]byte, error) {
	b.m.RLock()
	defer b.m.RUnlock()

	type Alias Balancer

	return json.Marshal(&struct {
		Alive   []*Server `json:"alive"`
		Proxies int       `json:"proxies"`
		*Alias
	}{
		Alive:   b.alive,
		Proxies: len(b.proxies),
		Alias:   (*Alias)(b),
	})
}

func (b *Balancer) fetchProxies() {
	var proxies []*url.URL

	writeLogFunc("fetching proxies")

	for schema, links := range b.Sources {
		for _, link := range links {
			resp, err := http.Get(link)
			if err != nil {
				writeLogFunc(fmt.Sprintf("error fetching proxies from %s: %v\n", link, err))
				continue
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				writeLogFunc(fmt.Sprintf("failed to download proxy list from %s: status %d\n", link, resp.StatusCode))
				continue
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				writeLogFunc(fmt.Sprintf("error reading response body from %s: %v\n", link, err))
				continue
			}

			for _, host := range strings.Split(string(body), "\n") {
				host = strings.TrimSpace(host)
				if host == "" {
					continue
				}

				if proxy, err := url.Parse(schema + "://" + host); err == nil && !slices.Contains(proxies, proxy) {
					proxies = append(proxies, proxy)
				}
			}
		}
	}

	b.m.Lock()
	b.proxies = proxies
	b.m.Unlock()
}

func (b *Balancer) checkProxies() {
	writeLogFunc(fmt.Sprintf("checking %d proxies", len(b.proxies)))

	var alive []*Server
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, proxy := range b.proxies {
		wg.Add(1)
		srv := &Server{URL: proxy}
		go func(s *Server) {
			defer wg.Done()
			if _, err := makeRequest(b.TestURL, b.UserAgent, b.Timeout, s); err == nil {
				mu.Lock()
				alive = append(alive, s)
				mu.Unlock()
			}
		}(srv)
	}
	wg.Wait()
	b.merge(alive)
}

func (b *Balancer) merge(s []*Server) {
	b.m.Lock()
	defer b.m.Unlock()

	servers := make([]*Server, 0, len(s))

	for _, new := range s {
		isNew := true

		for _, current := range b.alive {
			if current.URL == new.URL {
				isNew = false
				servers = append(servers, current)
				break
			}
		}

		if isNew {
			servers = append(servers, new)
		}
	}

	b.alive = servers
	writeLogFunc(fmt.Sprintf("merged %d alive proxies", len(servers)))
}

//  ███████╗███████╗██████╗ ██╗   ██╗███████╗██████╗
//  ██╔════╝██╔════╝██╔══██╗██║   ██║██╔════╝██╔══██╗
//  ███████╗█████╗  ██████╔╝██║   ██║█████╗  ██████╔╝
//  ╚════██║██╔══╝  ██╔══██╗╚██╗ ██╔╝██╔══╝  ██╔══██╗
//  ███████║███████╗██║  ██║ ╚████╔╝ ███████╗██║  ██║
//  ╚══════╝╚══════╝╚═╝  ╚═╝  ╚═══╝  ╚══════╝╚═╝  ╚═╝
//

type Server struct {
	URL      *url.URL `json:"url"`
	Weight   float64  `json:"weight"`
	Capacity int      `json:"capacity"`
	Latency  int      `json:"latency"`
	Requests int      `json:"requests"`
	Limit    int      `json:"limit"`
	Positive int      `json:"positive"`
	Negative int      `json:"negative"`

	m sync.RWMutex
}

func (s *Server) PositiveUp() {
	s.m.Lock()
	s.Positive++
	s.m.Unlock()
}

func (s *Server) MarshalJSON() ([]byte, error) {
	s.m.RLock()
	defer s.m.RUnlock()

	type Alias Server
	return json.Marshal(&struct {
		URL string `json:"url"`
		*Alias
	}{
		URL:   s.URL.Host,
		Alias: (*Alias)(s),
	})
}

func (s *Server) registerStart() time.Time {
	s.m.Lock()
	s.Requests++
	s.m.Unlock()
	return time.Now()
}

func (s *Server) registerFinish(startedAt time.Time) time.Time {
	endedAt := time.Now()
	s.m.Lock()
	s.Latency = int(endedAt.Sub(startedAt).Milliseconds())
	s.Requests--
	s.m.Unlock()
	return endedAt
}

//  ██████╗ ███████╗███████╗██████╗  ██████╗ ███╗   ██╗███████╗███████╗
//  ██╔══██╗██╔════╝██╔════╝██╔══██╗██╔═══██╗████╗  ██║██╔════╝██╔════╝
//  ██████╔╝█████╗  ███████╗██████╔╝██║   ██║██╔██╗ ██║███████╗█████╗
//  ██╔══██╗██╔══╝  ╚════██║██╔═══╝ ██║   ██║██║╚██╗██║╚════██║██╔══╝
//  ██║  ██║███████╗███████║██║     ╚██████╔╝██║ ╚████║███████║███████╗
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝      ╚═════╝ ╚═╝  ╚═══╝╚══════╝╚══════╝
//

type Response struct {
	Body      *bytes.Reader
	StartedAt time.Time
	EndedAt   time.Time
}

func (r *Response) Latency() int64 {
	return r.EndedAt.Sub(r.StartedAt).Milliseconds()
}

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

func sortAliveProxies(s []*Server) {
	if toggleSortProxies%2 == 0 {
		sort.Slice(s, func(i, j int) bool { return s[i].Weight < s[j].Weight })
	} else {
		sort.Slice(s, func(i, j int) bool { return s[i].Weight > s[j].Weight })
	}
	atomic.AddInt32(&toggleSortProxies, 1)
}

func computWeight(servers []*Server) float64 {
	totalWeight := 0.0
	for _, s := range servers {
		s.Weight = 1.0 / float64(s.Latency)
		totalWeight += s.Weight
	}
	return totalWeight
}

func computeCapacity(requests int, servers []*Server) {
	totalWeight := computWeight(servers)

	for _, s := range servers {
		pct := s.Weight / totalWeight
		if s.Limit > 0 {
			s.Capacity = s.Limit
		} else {
			s.Capacity = int(math.Round(pct * float64(requests)))
		}
	}
}

func bestServer(servers []*Server) *Server {
	var bs *Server
	for _, s := range servers {
		if s.Capacity > s.Requests {
			bs = s
			break
		}
	}
	return bs
}

func makeRequest(target string, agent string, timeout int, s *Server) (*Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(timeout))
	defer cancel()

	startedAt := s.registerStart()
	req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	req.Header.Set("User-Agent", agent)
	client := &http.Client{}
	client.Transport = &http.Transport{Proxy: http.ProxyURL(s.URL)}
	rsp, err := client.Do(req)
	endedAt := s.registerFinish(startedAt)

	r := &Response{nil, startedAt, endedAt}
	if err != nil {
		return r, err
	}
	defer rsp.Body.Close()

	body, err := io.ReadAll(rsp.Body)
	r.Body = bytes.NewReader(body)
	return r, err
}
