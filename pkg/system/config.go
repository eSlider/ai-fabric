package system

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"produktor.io/ai-fabric/pkg/file"
	giteadomain "produktor.io/ai-fabric/pkg/gitea"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
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
}

type Config struct {
	RootDir       string
	StateDir      string
	StatePath     string
	ApprovalsPath string
	TeaConfigDir  string
	Gitea         giteadomain.Config
	Issue         IssueConfig
}

// LoadEnvs from path
func LoadEnvs(path string) (err error) {
	path = path + "/" // Configuration RootPath
	// Load default environment variables
	if err := godotenv.Load(path+".env", path+".env.default"); err != nil {
		return err
	}
	return
}

// LoadProdEnvs loads environment variables for production from default and main configuration files.
func LoadProdEnvs() (err error) {
	// Load default environment variables
	ConfigPath := file.GetRootPath() + "/etc/"
	return godotenv.Overload(
		ConfigPath+".env.default",
		ConfigPath+".env",
		//ConfigPath+".env",
	)
}

// LoadTestEnvs loads environment variables for testing from default and test configuration files.
// Returns an error if loading any file fails.
func LoadTestEnvs() (err error) {
	// Load default environment variables
	ConfigPath := file.GetRootPath() + "/etc/"
	return godotenv.Overload(
		ConfigPath+".env.default",
		ConfigPath+".env.test",
		//ConfigPath+".env",
	)
}

// GetTempDir returns the path to the system's temporary directory.
// Generate random name using OS-specific UUID
func GetTempDir() string {
	uq := uuid.New().String()
	path := file.GetRootPath() + "/var/temp/" + uq
	err := os.MkdirAll(path, 0755)
	if err != nil {
		panic(err)
	}
	return path
}

// LoadMerged loads and merges environment variables from default and custom .env files.
// It attempts to load ".env.default" and ".env" from the configuration directory.
// Returns an error if there are issues loading the files.
func LoadMerged() (err error) {
	// Load default environment variables
	ConfigPath := file.GetRootPath() + "/etc/"
	return godotenv.Overload(
		ConfigPath+".env.default",
		ConfigPath+".env",
	)
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

	// Compatibility overrides for legacy and documented env names.
	if v := os.Getenv("GITEA_BOT_BASE_URL"); v != "" {
		cfg.Gitea.BaseURL = v
	}
	if v := os.Getenv("GITEA_BOT_OWNER"); v != "" {
		cfg.Gitea.Owner = v
	}
	if v := os.Getenv("GITEA_BOT_REPO"); v != "" {
		cfg.Gitea.Repo = v
	}
	if v := os.Getenv("GITEA_BOT_TOKEN"); v != "" {
		cfg.Gitea.Token = v
	}
	if v := os.Getenv("GITEA_CLI_DOCKER_NETWORK"); v != "" {
		cfg.Gitea.CLI.Docker.Network = v
	}
	if v := os.Getenv("GITEA_CLI_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Gitea.UseCLI = b
			if os.Getenv("GITEA_TRANSPORT_PRIMARY") == "" && os.Getenv("GITEA_PRIMARY_TRANSPORT") == "" {
				if b {
					cfg.Gitea.PrimaryTransport = "cli"
				} else {
					cfg.Gitea.PrimaryTransport = "sdk"
				}
			}
		}
	}
	if v := os.Getenv("GITEA_TRANSPORT_PRIMARY"); v != "" {
		cfg.Gitea.PrimaryTransport = v
	}
	if v := os.Getenv("GITEA_PRIMARY_TRANSPORT"); v != "" {
		cfg.Gitea.PrimaryTransport = v
	}
	if v := os.Getenv("GITEA_CLI_FALLBACK_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Gitea.CLIFallbackEnabled = b
		}
	}
	if v := os.Getenv("GITEA_TRANSPORT_CLI_FALLBACK"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Gitea.CLIFallbackEnabled = b
		}
	}
	if v := os.Getenv("ISSUE_POLL_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Issue.IssueBot.PollInterval = n
		}
	}
	if v := os.Getenv("ISSUE_HANDLER_DRY_RUN"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Issue.DryRun = b
		}
	}

	cfg.Gitea.Normalize()

	// Post-processing
	stateDir := filepath.Join(cfg.RootDir, "var", "issue-handler")
	if os.Getenv("FABRIC_STATE_DIR") != "" {
		stateDir = os.Getenv("FABRIC_STATE_DIR")
	}
	_ = os.MkdirAll(stateDir, 0755)

	cfg.StateDir = stateDir
	cfg.StatePath = filepath.Join(stateDir, "state.json")
	cfg.ApprovalsPath = filepath.Join(stateDir, "approvals.json")
	cfg.TeaConfigDir = filepath.Join(stateDir, "tea-config")

	return cfg
}
