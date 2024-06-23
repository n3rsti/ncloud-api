package utils

import (
	"reflect"
	"runtime"
)

func GetFuncName(f interface{}) string {
	rv := reflect.ValueOf(f)

	funcName := runtime.FuncForPC(rv.Pointer()).Name()
	return funcName
}
