package httptines

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

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
