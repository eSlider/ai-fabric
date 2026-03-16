package main

import (
	"flag"
	"log"
	"time"

	"example.org/ai-fabric/internal/config"
	"example.org/ai-fabric/pkg/fabric"
)

func main() {
	once := flag.Bool("once", false, "Run a single polling cycle")
	issueNumber := flag.Int("issue-number", 0, "Process only a specific issue number")
	flag.Parse()

	cfg := config.LoadConfig()

	if cfg.Gitea.Token == "" {
		log.Fatal("GITEA_BOT_TOKEN is required in environment")
	}

	handler := fabric.NewIssueHandler(cfg)

	if *once {
		handler.RunOnce(*issueNumber)
	} else {
		for {
			handler.RunOnce(*issueNumber)
			time.Sleep(time.Duration(cfg.Issue.IssueBot.PollInterval) * time.Second)
		}
	}
}
