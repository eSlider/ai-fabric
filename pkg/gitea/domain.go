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
