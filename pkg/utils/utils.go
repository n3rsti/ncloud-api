package utils

import (
	"encoding/json"
	"fmt"
	"ncloud-api/pkg/validator"
	"net/http"
	"reflect"
	"runtime"
)

func GetFuncName(f interface{}) string {
	rv := reflect.ValueOf(f)

	funcName := runtime.FuncForPC(rv.Pointer()).Name()
	return funcName
}

func Encode[T any](w http.ResponseWriter, r *http.Request, status int, v T) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}

	return nil
}

func Decode[T any](r *http.Request) (T, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return v, fmt.Errorf("decode json: %w", err)
	}

	return v, nil
}

func DecodeValid[T validator.Validator](r *http.Request) (T, map[string]string, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return v, nil, fmt.Errorf("decode json: %w", err)
	}

	if problems := v.Valid(); len(problems) > 0 {
		return v, problems, nil
	}

	return v, nil, nil
}
