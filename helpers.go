package httptines

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

func doRequest(target string, s *server, timeout int, agent string) (*bytes.Reader, error) {
	startAt := s.startRequest()
	defer func() { s.finishRequest(startAt) }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*time.Duration(timeout))
	defer cancel()

	client := &http.Client{}
	client.Transport = &http.Transport{Proxy: http.ProxyURL(s.url)}

	req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	req.Header.Set("User-Agent", agent)

	resp, err := client.Do(req)
	if resp == nil || err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	return bytes.NewReader(b), err
}

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
