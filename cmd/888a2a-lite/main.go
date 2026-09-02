package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tbdavid2019/888a2a-lite/internal/config"
	"github.com/tbdavid2019/888a2a-lite/internal/hub"
	"github.com/tbdavid2019/888a2a-lite/internal/service"
	"github.com/tbdavid2019/888a2a-lite/internal/store"
	"github.com/tbdavid2019/888a2a-lite/internal/store/sqlite"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "server"
	if len(args) > 0 {
		command = args[0]
		args = args[1:]
	}
	if command != "server" {
		return runCLI(command, args)
	}
	return runServer(args)
}

func runServer(args []string) error {
	flags := flag.NewFlagSet("server", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	listenAddr := flags.String("listen", "", "override the configured HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx := context.Background()
	database, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()
	repository := sqlite.NewRepository(database)
	defaultPolicy := hub.HubPolicy{
		HubID:               cfg.HubID,
		RegistrationEnabled: cfg.RegistrationEnabled,
		RegistrationTTL:     cfg.RegistrationTTL,
		PeerLease:           cfg.PeerLease,
		MaxRegisteredAgents: cfg.MaxRegisteredAgents,
		MaxTasksPerMinute:   cfg.MaxTasksPerMinute,
		MaxConcurrentTasks:  cfg.MaxConcurrentTasks,
		MaxPayloadBytes:     cfg.MaxPayloadBytes,
	}
	if _, err := repository.Policy().GetPolicy(ctx); errors.Is(err, store.ErrNotFound) {
		if err := repository.Policy().SavePolicy(ctx, defaultPolicy); err != nil {
			return fmt.Errorf("initialize hub policy: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read hub policy: %w", err)
	}
	hubService := service.New(repository, cfg)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           service.NewHTTPServer(hubService).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}()
	log.Printf("888a2a-lite listening on %s", cfg.ListenAddr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("serve hub: %w", err)
	}
	return nil
}
