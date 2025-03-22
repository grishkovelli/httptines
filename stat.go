package httptines

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Stat represents the global statistics for the application
type Stat struct {
	// Targets is the total number of URLs to process
	Targets int `json:"targets"`
	// RPM represents the current requests per minute
	RPM int `json:"rpm"`
	// Servers contains a map of active proxy servers and their statistics
	Servers map[string]any `json:"servers"`

	m          sync.RWMutex
	timestamps []time.Time
}

// MarshalJSON implements the json.Marshaler interface for Stat
// Returns:
//   - []byte: JSON representation of the statistics
//   - error: Any error that occurred during marshaling
func (s *Stat) MarshalJSON() ([]byte, error) {
	type Alias Stat

	return json.Marshal(&struct {
		RPM       int    `json:"rpm"`
		Processed int    `json:"processed"`
		Elapsed   string `json:"elapsed"`
		*Alias
	}{
		RPM:       s.rpm(),
		Processed: len(s.timestamps),
		Elapsed:   s.elapsed(),
		Alias:     (*Alias)(s),
	})
}

// rpm calculates the current requests per minute based on successful requests
// Returns:
//   - int: Number of successful requests in the last minute
func (s *Stat) rpm() int {
	rpm, lastMinute := 0, time.Now().Add(-time.Minute)
	for i := len(s.timestamps) - 1; i >= 0; i-- {
		if s.timestamps[i].Compare(lastMinute) < 0 {
			break
		}
		rpm++
	}
	return rpm
}

// addServer adds or updates server statistics
// Parameters:
//   - data: Map containing server statistics
func (s *Stat) addServer(data map[string]any) {
	s.m.Lock()
	if url, ok := data["url"].(string); ok {
		s.Servers[url] = data
	}
	s.m.Unlock()
}

// addTimestamp adds a timestamp for successful requests
// Parameters:
//   - t: Time of the successful request
func (s *Stat) addTimestamp(t time.Time) {
	s.m.Lock()
	s.timestamps = append(s.timestamps, t)
	s.m.Unlock()
}

func (s *Stat) allTargetsProcessed() bool {
	s.m.RLock()
	defer s.m.RUnlock()

	return len(s.timestamps) == s.Targets
}

func (s *Stat) elapsed() string {
	if tLen := len(s.timestamps); tLen > 1 {
		elapsed := int(s.timestamps[tLen-1].Sub(s.timestamps[0]).Seconds())
		minutes := elapsed / 60
		seconds := elapsed % 60
		return fmt.Sprintf("%02d:%02d", minutes, seconds)
	}
	return "00:00"
}
