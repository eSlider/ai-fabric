package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"example.org/ai-fabric/pkg/file"
	giteadomain "example.org/ai-fabric/pkg/gitea"

	"github.com/go-viper/mapstructure/v2"
	"gopkg.in/yaml.v3"
)

type ArchitectConfig struct {
	Enabled  bool
	MaxChars int
	// MaxAttempts bounds failed architect runs per issue before needs_human.
	MaxAttempts int
}

type IssueBot struct {
	PollInterval     int
	MaxFixAttempts   int
	RetryIntervalSec int
	TelegramBotToken string
}

type WebhookConfig struct {
	// Secret validates X-Gitea-Signature on incoming webhooks (empty disables validation).
	Secret string
	// CIFixMaxPerSHA limits developer fix attempts per PR head commit.
	CIFixMaxPerSHA int
	// CIFixMaxPerPR limits total developer fix attempts per PR across commits.
	CIFixMaxPerPR int
}

type IssueConfig struct {
	IssueBot
	BaseBranch     string
	AgentBin       string
	AgentExtraArgs string
	SmartModel     string
	DryRun         bool
	// InProgressTimeoutSec marks a status:in_progress issue as stale and reclaimable.
	InProgressTimeoutSec int
	// AgentTimeoutSec is the hard deadline for a single agent run.
	AgentTimeoutSec int
	Architect       ArchitectConfig
	Webhook         WebhookConfig
	Poll            struct {
		Interval struct {
			Sec *int
		}
	}
	Handler struct {
		Dry struct {
			Run *bool
		}
	}
}

type BotConfig struct {
	Token            string
	AllowedChatIDs   map[string]bool
	AllowedUsers     map[string]bool
	GiteaBaseURL     string
	GiteaOwner       string
	GiteaRepo        string
	GiteaToken       string
	MCPBaseURL       string
	MCPAccessToken   string
	ProjectListLimit int
}

type Config struct {
	RootDir   string
	StateDir  string
	StatePath string
	Telegram  struct {
		Bot struct {
			Token string
		}
		Allowed struct {
			Chat struct {
				IDs string
			}
			Usernames string
		}
	}
	Project struct {
		List struct {
			Limit int
		}
	}
	Fabric struct {
		State struct {
			Dir string
		}
	}
	Gitea giteadomain.Config
	Issue IssueConfig
}

// ReadConfig from path
func ReadConfig(path string, env string, pointer any) (err error) {

	readFile, err := os.ReadFile(path)

	if err != nil {
		return err
	}

	var yml map[string]map[string]any
	err = yaml.Unmarshal(readFile, &yml)

	if yml[env] == nil {
		return errors.New("no environment")
	}

	input := yml[env]
	err = mapstructure.WeakDecode(input, &pointer)
	return
}

// LoadBotConfig loads telegram bot configuration from environment variables.
// It uses UnmarshalEnvironment smart decoding and maps decoded values to runtime config.
func LoadBotConfig() BotConfig {
	var cfg Config
	_ = UnmarshalEnvironment(&cfg)

	allowedChatIDs := map[string]bool{}
	if v := cfg.Telegram.Allowed.Chat.IDs; v != "" {
		allowedChatIDs = parseSet(v)
	}

	allowedUsers := map[string]bool{}
	if v := cfg.Telegram.Allowed.Usernames; v != "" {
		allowedUsers = parseSet(strings.ToLower(v))
	}

	projectListLimit := 20
	if cfg.Project.List.Limit > 0 {
		projectListLimit = cfg.Project.List.Limit
	}

	return BotConfig{
		Token:            cfg.Telegram.Bot.Token,
		AllowedChatIDs:   allowedChatIDs,
		AllowedUsers:     allowedUsers,
		GiteaBaseURL:     stringOrDefault(cfg.Gitea.Bot.Base.URL, "http://localhost:3000"),
		GiteaOwner:       stringOrDefault(cfg.Gitea.Bot.Owner, "eslider"),
		GiteaRepo:        stringOrDefault(cfg.Gitea.Bot.Repo, "ai-fabric"),
		GiteaToken:       cfg.Gitea.Bot.Token,
		MCPBaseURL:       stringOrDefault(cfg.Gitea.Mcp.Base.URL, "http://localhost:8080/mcp"),
		MCPAccessToken:   cfg.Gitea.Access.Token,
		ProjectListLimit: projectListLimit,
	}
}

func parseSet(v string) map[string]bool {
	out := map[string]bool{}
	for part := range strings.SplitSeq(v, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func stringOrDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// LoadConfig loads the issue handler configuration from environment variables.
func LoadConfig() *Config {
	rootDir := file.GetRootPath()

	cfg := &Config{
		RootDir: rootDir,
		Gitea: giteadomain.Config{
			BotConfig: giteadomain.BotConfig{
				BaseURL: "http://localhost:3000",
				Owner:   "eslider",
				Repo:    "ai-fabric",
			},
		},
		Issue: IssueConfig{
			IssueBot: IssueBot{
				PollInterval:     45,
				MaxFixAttempts:   3,
				RetryIntervalSec: 600,
			},
			BaseBranch:           "main",
			AgentBin:             "agent",
			InProgressTimeoutSec: 3600,
			AgentTimeoutSec:      1800,
			Architect: ArchitectConfig{
				Enabled:     true,
				MaxChars:    6000,
				MaxAttempts: 2,
			},
			Webhook: WebhookConfig{
				CIFixMaxPerSHA: 2,
				CIFixMaxPerPR:  6,
			},
		},
	}

	_ = UnmarshalEnvironment(cfg)

	setStringIfNotEmpty(&cfg.Gitea.BaseURL, cfg.Gitea.Bot.Base.URL)
	setStringIfNotEmpty(&cfg.Gitea.Owner, cfg.Gitea.Bot.Owner)
	setStringIfNotEmpty(&cfg.Gitea.Repo, cfg.Gitea.Bot.Repo)
	setStringIfNotEmpty(&cfg.Gitea.Token, cfg.Gitea.Bot.Token)
	setStringIfNotEmpty(&cfg.Gitea.HandlerToken, os.Getenv("GITEA_HANDLER_TOKEN"))
	setStringIfNotEmpty(&cfg.Gitea.ReviewerToken, os.Getenv("GITEA_REVIEWER_TOKEN"))
	setStringIfNotEmpty(&cfg.Gitea.ArchitectToken, os.Getenv("GITEA_ARCHITECT_TOKEN"))
	if cfg.Gitea.HandlerToken == "" {
		cfg.Gitea.HandlerToken = cfg.Gitea.Token
	}
	if cfg.Gitea.ReviewerToken == "" {
		cfg.Gitea.ReviewerToken = cfg.Gitea.Token
	}
	if cfg.Gitea.ArchitectToken == "" {
		cfg.Gitea.ArchitectToken = cfg.Gitea.Token
	}

	setStringIfNotEmpty(&cfg.Issue.Webhook.Secret, os.Getenv("GITEA_WEBHOOK_SECRET"))
	setIntFromEnv(&cfg.Issue.InProgressTimeoutSec, "ISSUE_IN_PROGRESS_TIMEOUT_SEC")
	setIntFromEnv(&cfg.Issue.AgentTimeoutSec, "ISSUE_AGENT_TIMEOUT_SEC")
	setIntFromEnv(&cfg.Issue.Architect.MaxAttempts, "ISSUE_ARCHITECT_MAX_ATTEMPTS")

	if cfg.Issue.Poll.Interval.Sec != nil && *cfg.Issue.Poll.Interval.Sec > 0 {
		cfg.Issue.IssueBot.PollInterval = *cfg.Issue.Poll.Interval.Sec
	}
	setBoolIfNotNil(&cfg.Issue.DryRun, cfg.Issue.Handler.Dry.Run)

	// Post-processing
	stateDir := filepath.Join(cfg.RootDir, "var", "issue-handler")
	if cfg.Fabric.State.Dir != "" {
		stateDir = cfg.Fabric.State.Dir
	}
	_ = os.MkdirAll(stateDir, 0755)

	cfg.StateDir = stateDir

	return cfg
}

func setStringIfNotEmpty(dst *string, src string) {
	if src != "" {
		*dst = src
	}
}

func setBoolIfNotNil(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

func setIntFromEnv(dst *int, key string) {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			*dst = n
		}
	}
}
