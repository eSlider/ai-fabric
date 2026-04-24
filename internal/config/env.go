package config

import (
	"produktor.io/ai-fabric/pkg/file"

	goenv "github.com/eslider/go-env"
	"github.com/joho/godotenv"
)

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

// UnmarshalEnvironment read all environment variables and unmarshal them into the provided struct
// Something like SERVICE_KEY=1234567890 would be unmarshal into the struct Service.Key = "1234567890"
//
// Implementation delegated to github.com/eslider/go-env (extracted per
// inventar/docs/asr/ASR-0008-ai-fabric-audit.md).
func UnmarshalEnvironment(s any) error {
	return goenv.Unmarshal(s)
}
