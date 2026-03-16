package gitea

import "strings"

type CLIDockerConfig struct {
	Network string
}

type CLIConfig struct {
	Bin    string
	Image  string
	Login  string
	URL    string
	Token  string
	Docker CLIDockerConfig
	Enabled *bool
	Fallback struct {
		Enabled *bool
	}
}

type BotConfig struct {
	BaseURL string
	Owner   string
	Repo    string
	Token   string
}

type Config struct {
	BotConfig
	UseCLI             bool
	PrimaryTransport   string
	CLIFallbackEnabled bool
	CLI                CLIConfig
	Bot                struct {
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
	Transport struct {
		Primary string
		Cli     struct {
			Fallback *bool
		}
	}
	Primary struct {
		Transport string
	}
}

func (c *Config) Normalize() {
	c.PrimaryTransport = strings.ToLower(strings.TrimSpace(c.PrimaryTransport))
	switch c.PrimaryTransport {
	case "cli", "sdk":
	default:
		c.PrimaryTransport = "sdk"
	}

	if c.CLI.URL == "" {
		c.CLI.URL = c.BaseURL
	}
	if c.CLI.Token == "" {
		c.CLI.Token = c.Token
	}
}
