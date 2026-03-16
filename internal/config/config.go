package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"produktor.io/ai-fabric/pkg/file"
	giteadomain "produktor.io/ai-fabric/pkg/gitea"

	"github.com/mitchellh/mapstructure"
	"gopkg.in/yaml.v3"
)

type ArchitectConfig struct {
	Enabled  bool
	MaxChars int
}

type IssueBot struct {
	PollInterval     int
	MaxFixAttempts   int
	RetryIntervalSec int
	TelegramBotToken string
}

type IssueConfig struct {
	IssueBot
	BaseBranch     string
	AgentBin       string
	AgentExtraArgs string
	DryRun         bool
	Architect      ArchitectConfig
	Poll           struct {
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
	RootDir       string
	StateDir      string
	StatePath     string
	ApprovalsPath string
	TeaConfigDir  string
	Telegram      struct {
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
func ReadConfig(path string, env string, pointer interface{}) (err error) {

	readFile, err := os.ReadFile(path)

	if err != nil {
		return err
	}

	var yml map[string]map[string]interface{}
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
	for _, part := range strings.Split(v, ",") {
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
			UseCLI:             true,
			PrimaryTransport:   "cli",
			CLIFallbackEnabled: true,
			CLI: giteadomain.CLIConfig{
				Image: "gitea/tea:latest",
				Login: "ai-fabric",
				Docker: giteadomain.CLIDockerConfig{
					Network: "host",
				},
			},
		},
		Issue: IssueConfig{
			IssueBot: IssueBot{
				PollInterval:     45,
				MaxFixAttempts:   3,
				RetryIntervalSec: 600,
			},
			BaseBranch: "main",
			AgentBin:   "agent",
			Architect: ArchitectConfig{
				Enabled:  true,
				MaxChars: 6000,
			},
		},
	}

	_ = UnmarshalEnvironment(cfg)

	setStringIfNotEmpty(&cfg.Gitea.BaseURL, cfg.Gitea.Bot.Base.URL)
	setStringIfNotEmpty(&cfg.Gitea.Owner, cfg.Gitea.Bot.Owner)
	setStringIfNotEmpty(&cfg.Gitea.Repo, cfg.Gitea.Bot.Repo)
	setStringIfNotEmpty(&cfg.Gitea.Token, cfg.Gitea.Bot.Token)

	if cfg.Gitea.CLI.Enabled != nil {
		cfg.Gitea.UseCLI = *cfg.Gitea.CLI.Enabled
		if cfg.Gitea.Transport.Primary == "" && cfg.Gitea.Primary.Transport == "" {
			if cfg.Gitea.UseCLI {
				cfg.Gitea.PrimaryTransport = "cli"
			} else {
				cfg.Gitea.PrimaryTransport = "sdk"
			}
		}
	}
	setStringIfNotEmpty(&cfg.Gitea.PrimaryTransport, cfg.Gitea.Transport.Primary)
	setStringIfNotEmpty(&cfg.Gitea.PrimaryTransport, cfg.Gitea.Primary.Transport)

	setBoolIfNotNil(&cfg.Gitea.CLIFallbackEnabled, cfg.Gitea.CLI.Fallback.Enabled)
	setBoolIfNotNil(&cfg.Gitea.CLIFallbackEnabled, cfg.Gitea.Transport.Cli.Fallback)
	if cfg.Issue.Poll.Interval.Sec != nil && *cfg.Issue.Poll.Interval.Sec > 0 {
		cfg.Issue.IssueBot.PollInterval = *cfg.Issue.Poll.Interval.Sec
	}
	setBoolIfNotNil(&cfg.Issue.DryRun, cfg.Issue.Handler.Dry.Run)

	cfg.Gitea.Normalize()

	// Post-processing
	stateDir := filepath.Join(cfg.RootDir, "var", "issue-handler")
	if cfg.Fabric.State.Dir != "" {
		stateDir = cfg.Fabric.State.Dir
	}
	_ = os.MkdirAll(stateDir, 0755)

	cfg.StateDir = stateDir
	cfg.StatePath = filepath.Join(stateDir, "state.json")
	cfg.ApprovalsPath = filepath.Join(stateDir, "approvals.json")
	cfg.TeaConfigDir = filepath.Join(stateDir, "tea-config")

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
