package gitea

// BotConfig holds the flattened, resolved connection settings.
type BotConfig struct {
	BaseURL        string
	Owner          string
	Repo           string
	Token          string
	HandlerToken   string
	ReviewerToken  string
	ArchitectToken string
}

// Config is the env-mapped Gitea configuration.
// Nested structs mirror environment variable paths (e.g. GITEA_BOT_BASE_URL -> Bot.Base.URL);
// resolved values live in the embedded BotConfig.
type Config struct {
	BotConfig
	Bot struct {
		Base struct {
			URL string
		}
		Owner string
		Repo  string
		Token string
	}
	Mcp struct {
		Base struct {
			URL string
		}
	}
	Access struct {
		Token string
	}
}
