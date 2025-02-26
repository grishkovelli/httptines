package wlpb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	. "github.com/bsm/ginkgo/v2"
	. "github.com/bsm/gomega"
)

func TestWlpb(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "wlpb")
}

//  ████████╗███████╗███████╗████████╗     ██████╗ █████╗ ███████╗███████╗███████╗
//  ╚══██╔══╝██╔════╝██╔════╝╚══██╔══╝    ██╔════╝██╔══██╗██╔════╝██╔════╝██╔════╝
//     ██║   █████╗  ███████╗   ██║       ██║     ███████║███████╗█████╗  ███████╗
//     ██║   ██╔══╝  ╚════██║   ██║       ██║     ██╔══██║╚════██║██╔══╝  ╚════██║
//     ██║   ███████╗███████║   ██║       ╚██████╗██║  ██║███████║███████╗███████║
//     ╚═╝   ╚══════╝╚══════╝   ╚═╝        ╚═════╝╚═╝  ╚═╝╚══════╝╚══════╝╚══════╝
//

var _ = Describe("Balancer.Run", func() {
	var (
		proxy       *httptest.Server
		proxyURL    *url.URL
		src, target *httptest.Server
		b           *Balancer
		logs        []string
		mu          sync.Mutex
	)

	logFunc := func(m string) {
		mu.Lock()
		logs = append(logs, m)
		mu.Unlock()
	}

	BeforeEach(func() {
		proxy, proxyURL = mockProxyServer()
		src = mockHTTPServer(proxyURL.Host + "\n")
		target = mockHTTPServer("mocked content")

		b = &Balancer{
			Requests:    10,
			Periodicity: 1,
			Sources:     map[string][]string{"http": {src.URL}},
			TestURL:     target.URL,
			Timeout:     10,
			UserAgent:   "httptines 1.0",
		}
	})

	AfterEach(func() {
		proxy.Close()
		src.Close()
		target.Close()
	})

	It("tests proxy fetching, checking, merging, and logging", func() {
		Expect(b.proxies).To(HaveLen(0))
		Expect(b.alive).To(HaveLen(0))

		go b.Run(logFunc)
		time.Sleep(1 * time.Second)

		Expect(b.proxies).To(HaveLen(1))
		Expect(b.alive).To(HaveLen(1))
		Expect(b.alive[0].Latency).To(BeNumerically("~", 10, 5))

		Expect(logs[0]).To(Equal("fetching proxies"))
		Expect(logs[1]).To(Equal("checking 1 proxies"))
		Expect(logs[2]).To(Equal("merged 1 alive proxies"))
		Expect(logs[3]).To(Equal("fetching proxies"))
	})
})

var _ = Describe("Balanser.NextServer", func() {
	b := createBalancer()

	It("returns server", func() {
		var s *Server

		toggleSortProxies = 0

		// returns server with minimal latency (toggleSortProxies is zero or even number)
		s = b.NextServer()
		s.Requests++
		Expect(s.Latency).To(Equal(99))

		// returns server with maximal latency (toggleSortProxies is odd number)
		s = b.NextServer()
		s.Requests++
		Expect(s.Latency).To(Equal(9000))

		// returns last availible server
		s = b.NextServer()
		s.Requests++

		// returns nil if all servers are busy
		Expect(b.NextServer()).To(BeNil())
	})
})

var _ = Describe("Balancer.Request", func() {
	var (
		s *Server
		b *Balancer
		t *httptest.Server
	)

	BeforeEach(func() {
		s = &Server{Latency: 1, Capacity: 5}
		b = &Balancer{Requests: 10, Timeout: 2, alive: []*Server{s}}
		t = mockHTTPServer("Mocked Response")
	})

	AfterEach(func() {
		t.Close()
	})

	It("returns successful response", func() {
		proxy, proxyURL := mockProxyServer()
		defer proxy.Close()

		s.URL = proxyURL
		body, err, ok := b.Request(t.URL, "testAgent")

		Expect(ok).To(BeTrue())
		Expect(err).To(BeNil())
		Expect(string(body)).To(Equal("Mocked Response"))
		Expect(s.Requests).To(Equal(0))
		Expect(s.Positive).To(Equal(1))
		Expect(s.Negative).To(Equal(0))
	})

	When("proxy returns 502", func() {
		It("returns error", func() {
			proxy, proxyURL := mockProxyServerWith502()
			defer proxy.Close()

			s.URL = proxyURL
			_, err, ok := b.Request(t.URL, "testAgent")

			Expect(ok).To(BeTrue())
			Expect(err).NotTo(BeNil())
			Expect(err).To(MatchError(ContainSubstring("unexpected status code: 502")))
			Expect(s.Requests).To(Equal(0))
			Expect(s.Positive).To(Equal(0))
			Expect(s.Negative).To(Equal(1))
		})
	})

	When("timeout is reached", func() {
		It("returns error", func() {
			proxy, proxyURL := mockSlowlyProxyServer()
			defer proxy.Close()

			go func() {
				time.Sleep(50 * time.Millisecond)
				Expect(s.Requests).To(Equal(1))
			}()

			s.URL = proxyURL
			_, err, ok := b.Request(t.URL, "testAgent")

			Expect(ok).To(BeTrue())
			Expect(err).To(MatchError(context.DeadlineExceeded))
			Expect(s.Positive).To(Equal(0))
			Expect(s.Negative).To(Equal(1))
		})
	})
})

var _ = Describe("Balancer.MarshalJSON", func() {
	b := createBalancer()
	b.alive = b.alive[:1]

	It("returns JSON representation of the server", func() {
		Expect(b.MarshalJSON()).To(MatchJSON(`{
      "alive": [
        {
          "url": "4.4.4.4:8080",
					"negativePct": 0,
          "capacity": 471,
          "latency": 99,
          "requests": 470,
          "limit": 0,
          "positive": 1000,
          "negative": 1
        }
      ],
      "proxies": 1,
      "requests": 500,
      "periodicity": 120,
			"rpm": 0,
      "positive": 0,
      "sources": {
        "http": [
          "http://source.1"
        ]
      },
      "testURL": "http://target.url",
      "timeout": 10,
      "userAgent": "default"
    }`))
	})
})

var _ = Describe("computeCapacity", func() {
	When("no limits", func() {
		It("distributes requests based on latency", func() {
			b := createBalancer()
			computeCapacity(500, b.alive)

			Expect(b.alive[0].Capacity).To(Equal(471))
			Expect(b.alive[1].Capacity).To(Equal(30))
			Expect(b.alive[2].Capacity).To(Equal(5))
		})
	})

	When("with limits", func() {
		It("distributes requests based on latency and servers limits", func() {
			b := createBalancer()

			b.alive[0].Limit = 100

			computeCapacity(500, b.alive)

			Expect(b.alive[0].Capacity).To(Equal(100))
			Expect(b.alive[1].Capacity).To(Equal(30))
			Expect(b.alive[2].Capacity).To(Equal(5))
		})
	})
})

var _ = Describe("Server.MarshalJSON", func() {
	proxy, _ := url.Parse("http://4.4.4.4:8080")
	s := &Server{URL: proxy, Weight: 3, Capacity: 5, Latency: 3000, Requests: 7, Limit: 7, Positive: 100, Negative: 103}

	It("returns JSON representation of the Server", func() {
		Expect(s.MarshalJSON()).To(MatchJSON(`{
			"url": "4.4.4.4:8080",
			"negativePct": 51,
			"capacity": 5,
			"latency": 3000,
			"requests": 7,
			"limit": 7,
			"positive": 100,
			"negative": 103
		}`))
	})
})

var _ = Describe("Server.RegisterStart / RegisterFinish", func() {
	var s *Server

	BeforeEach(func() {
		s = &Server{Capacity: 5, Requests: 10}
	})

	When("negative response", func() {
		It("changes attributes", func() {
			t := s.RegisterStart()
			Expect(s.Requests).To(Equal(11))

			// simulate request
			time.Sleep(100 * time.Millisecond)
			s.RegisterFinish(t, errors.New("some error"))

			Expect(s.Requests).To(Equal(10))
			Expect(s.Limit).To(Equal(10))
			Expect(s.Positive).To(Equal(0))
			Expect(s.Negative).To(Equal(1))
			Expect(s.Latency).To(BeNumerically("~", 100, 5))
		})
	})

	When("positive response", func() {
		It("changes attributes", func() {
			t := s.RegisterStart()
			Expect(s.Requests).To(Equal(11))

			// simulate request
			time.Sleep(100 * time.Millisecond)
			s.RegisterFinish(t, nil)

			Expect(s.Requests).To(Equal(10))
			Expect(s.Limit).To(Equal(0))
			Expect(s.Positive).To(Equal(1))
			Expect(s.Negative).To(Equal(0))
			Expect(s.Latency).To(BeNumerically("~", 100, 5))
		})
	})
})

//  ██╗  ██╗███████╗██╗     ██████╗ ███████╗██████╗ ███████╗
//  ██║  ██║██╔════╝██║     ██╔══██╗██╔════╝██╔══██╗██╔════╝
//  ███████║█████╗  ██║     ██████╔╝█████╗  ██████╔╝███████╗
//  ██╔══██║██╔══╝  ██║     ██╔═══╝ ██╔══╝  ██╔══██╗╚════██║
//  ██║  ██║███████╗███████╗██║     ███████╗██║  ██║███████║
//  ╚═╝  ╚═╝╚══════╝╚══════╝╚═╝     ╚══════╝╚═╝  ╚═╝╚══════╝
//

func createBalancer() *Balancer {
	proxy, _ := url.Parse("http://4.4.4.4:8080")

	return &Balancer{
		Requests:    500,
		Periodicity: 120,
		Sources: map[string][]string{
			"http": {"http://source.1"},
		},
		TestURL:   "http://target.url",
		Timeout:   10,
		UserAgent: "default",
		alive: []*Server{
			{URL: proxy, Latency: 99, Capacity: 471, Requests: 470, Limit: 0, Positive: 1000, Negative: 1},
			{URL: proxy, Latency: 2000, Capacity: 30, Requests: 29, Limit: 30, Positive: 100, Negative: 50},
			{URL: proxy, Latency: 9000, Capacity: 7, Requests: 4, Limit: 5, Positive: 100, Negative: 103},
		},
		proxies: []*url.URL{proxy},
	}
}

func mockHTTPServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Write([]byte(body))
	}))
}

func mockProxyServer() (*httptest.Server, *url.URL) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp, err := http.Get(r.URL.String()) // Forward the request to the target
		if err != nil {
			http.Error(w, "Proxy Error", http.StatusBadGateway)
			return
		}

		// Copy the response from the target
		body, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		defer resp.Body.Close()
	}))
	u, _ := url.Parse(s.URL)
	return s, u
}

func mockProxyServerWith502() (*httptest.Server, *url.URL) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	u, _ := url.Parse(s.URL)
	return s, u
}

func mockSlowlyProxyServer() (*httptest.Server, *url.URL) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	u, _ := url.Parse(s.URL)
	return s, u
}
