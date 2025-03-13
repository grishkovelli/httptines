package httptines

import (
	"encoding/json"
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
		RPM       int `json:"rpm"`
		Processed int `json:"processed"`
		*Alias
	}{
		RPM:       s.rpm(),
		Processed: len(s.timestamps),
		Alias:     (*Alias)(s),
	})
}

// rpm calculates the current requests per minute based on successful requests
// Returns:
//   - int: Number of successful requests in the last minute
func (s *Stat) rpm() int {
	rpm, lastMinute := 0, time.Now().Add(-time.Minute)
	for i := len(s.timestamps) - 1; i >= 0; i-- {
		if s.timestamps[i].Compare(lastMinute) >= 0 {
			rpm++
		} else {
			break
		}
	}
	return rpm
}

// addServer adds or updates server statistics
// Parameters:
//   - data: Map containing server statistics
func (st *Stat) addServer(data map[string]any) {
	st.m.Lock()
	if url, ok := data["url"].(string); ok {
		st.Servers[url] = data
	}
	st.m.Unlock()
}

// addTimestamp adds a timestamp for successful requests
// Parameters:
//   - t: Time of the successful request
func (st *Stat) addTimestamp(t time.Time) {
	st.m.Lock()
	st.timestamps = append(st.timestamps, t)
	st.m.Unlock()
}

// removeServer removes server statistics
// Parameters:
//   - u: URL of the server to remove
func (st *Stat) removeServer(u string) {
	st.m.Lock()
	delete(st.Servers, u)
	st.m.Unlock()
}

// updateStat processes statistics updates from channels
func updateStat() {
	for {
		select {
		case d := <-statCh:
			stat.addServer(d)
		case d := <-timeCh:
			stat.addTimestamp(d)
		}
	}
}

// sendStatistics periodically broadcasts statistics to connected clients
func sendStatistics() {
	for {
		stat.m.RLock()
		p, _ := json.Marshal(Payload{"stat", stat})
		broadcast <- p
		stat.m.RUnlock()
		time.Sleep(time.Duration(cfg.statInterval) * time.Second)
	}
}
