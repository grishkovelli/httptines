package httptines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// wlog writes a log message to stdout and broadcasts it to connected clients.
// Parameters:
//   - s: Log message to write
func wlog(s string) {
	m := fmt.Sprintf("%s %s", time.Now().Format(time.DateTime), s)
	fmt.Println(m)
	p, _ := json.Marshal(Payload{"log", m})

	select {
	case broadcast <- p:
	default:
	}
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

// request makes an HTTP GET request to the target URL using the provided proxy server.
// Parameters:
//   - ctx: Context for the request
//   - target: URL to request
//   - s: Server to use for the request
//
// Returns:
//   - []byte: Response body
//   - error: Any error that occurred
func request(ctx context.Context, target string, s *Server) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", ua.get())

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(s.URL)},
		Timeout:   time.Duration(cfg.timeout) * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
