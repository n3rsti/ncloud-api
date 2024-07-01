package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"ncloud-api/logger"
	"ncloud-api/pkg/config"
	"ncloud-api/pkg/repositories"
	"ncloud-api/pkg/services"
)

// NewServer creates a new server, add routes and dependencies, returns http.Handler mux
func NewServer(
	logger *logger.Logger,
	userService *services.UserService,
) http.Handler {
	mux := http.NewServeMux()

	addRoutes(mux, userService)
	var handler http.Handler = mux
	handler = logger.Log(mux)

	return handler
}

// Run runs the server and creates shutdown context
func Run(ctx context.Context, w io.Writer, args []string) error {
	cfg := config.LoadConfig()

	// repositories
	userRepository := repositories.NewUserRepository(cfg.Db.Database(config.DbName))

	// services
	userService := services.NewUserService(userRepository)

	// middleware
	logger := logger.NewLogger(w)

	srv := NewServer(&logger, userService)

	httpServer := &http.Server{
		Addr:    net.JoinHostPort("0.0.0.0", "8080"),
		Handler: srv,
	}

	go func() {
		PrintRoutes()
		log.Printf("listening on %s\n", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "error listening and serving: %s\n", err)
		}

	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()

		shutdownCtx := context.Background()
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "error shutting down http server: %s\n", err)
		}
	}()

	wg.Wait()

	return nil
}
