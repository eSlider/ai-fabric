package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.org/ai-fabric/internal/config"
	"example.org/ai-fabric/pkg/fabric"
)

func main() {
	once := flag.Bool("once", false, "Run a single polling cycle")
	issueNumber := flag.Int("issue-number", 0, "Process only a specific issue number")
	flag.Parse()

	cfg := config.LoadConfig()
	log.Printf("[issue-handler] Config loaded. AgentBin: %s", cfg.Issue.AgentBin)

	if cfg.Gitea.Token == "" {
		log.Fatal("GITEA_BOT_TOKEN is required in environment")
	}

	handler := fabric.NewIssueHandler(cfg)

	if *once {
		handler.RunOnce(*issueNumber)
		return
	}

	// The webhook endpoint triggers agent runs; an unsigned endpoint on the
	// host network would let anyone start them.
	if cfg.Issue.Webhook.Secret == "" {
		log.Fatal("GITEA_WEBHOOK_SECRET is required: refusing to serve unsigned webhooks")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", handler.ServeWebhook)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: ":8082", Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("[issue-handler] Webhook server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Webhook server failed: %v", err)
		}
	}()

	pollInterval := time.Duration(cfg.Issue.IssueBot.PollInterval) * time.Second
	for {
		handler.RunOnce(*issueNumber)
		select {
		case <-ctx.Done():
			log.Printf("[issue-handler] Shutting down...")
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
			return
		case <-time.After(pollInterval):
		}
	}
}
