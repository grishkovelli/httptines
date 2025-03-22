package main

import (
	"fmt"
	"strconv"

	"github.com/grishkovelli/httptines"
)

// Any callback that do anything with response
func handleResponse(data []byte) {
	fmt.Printf("got %d bytes\n", len(data))
}

func main() {
	// Create slice of targets for web scraping
	targets := []string{}
	for i := range 500 {
		targets = append(targets, "https://httpstat.us/200?id="+strconv.Itoa(i+1))
	}

	worker := httptines.Worker{
		Timeout:    3,
		TestTarget: "https://httpstat.us",
		Sources: map[string][]string{
			"http": {
				"https://raw.githubusercontent.com/TheSpeedX/SOCKS-List/master/http.txt",
				"https://vakhov.github.io/fresh-proxy-list/http.txt",
				"https://raw.githubusercontent.com/monosans/proxy-list/refs/heads/main/proxies/http.txt",
				"https://raw.githubusercontent.com/proxifly/free-proxy-list/refs/heads/main/proxies/protocols/https/data.txt",
				"https://raw.githubusercontent.com/proxifly/free-proxy-list/refs/heads/main/proxies/protocols/http/data.txt",
				"https://raw.githubusercontent.com/ErcinDedeoglu/proxies/refs/heads/main/proxies/http.txt",
				"https://raw.githubusercontent.com/ErcinDedeoglu/proxies/refs/heads/main/proxies/https.txt",
			},
		},
	}

	worker.Run(targets, handleResponse)
}
