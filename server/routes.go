package server

import (
	"fmt"
	"net/http"

	"github.com/n3rsti/ncloud-api/utils"
)

// routes stores all the routes in format [path]   ->   [handler].
var routes []string

// handleHealth is used for health check, should return OK.
func handleHealth() http.Handler {
	response := []byte("OK\n")

	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write(response)
		},
	)
}

// addRoute adds the route based on [path] and [handler].
// Appends route in format [path]   ->   [handler] to routes slice.
func addRoute(mux *http.ServeMux, path string, handler http.Handler) {
	handlerName := utils.GetFuncName(handler)
	handlerDescription := fmt.Sprintf("%s   ->    %s", path, handlerName)

	routes = append(routes, handlerDescription)

	mux.Handle(path, handler)
}

// addRoutes adds all the routes with handlers.
func addRoutes(mux *http.ServeMux) {
	addRoute(mux, "GET /health", handleHealth())
}

// PrintRoutes prints routes from [routes] slice.
func PrintRoutes() {
	fmt.Println("Routes:")
	for _, route := range routes {
		fmt.Println(route)
	}
}
