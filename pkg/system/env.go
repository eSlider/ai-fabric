package system

import (
	"os"
	"strings"

	"github.com/mitchellh/mapstructure"
)

// UnmarshalEnvironment read all environment variables and unmarshal them into the provided struct
// Something like SERVICE_KEY=1234567890 would be unmarshal into the struct Service.Key = "1234567890"
func UnmarshalEnvironment(s any) (err error) {
	asMap := GetEnvVarsAsMap()
	return mapstructure.WeakDecode(asMap, s)
}

func GetEnvVarsAsMap() map[string]any {
	var envVars = make(map[string]any)

	// Get all environment variables
	for _, e := range os.Environ() {
		// Split variable values by _ and assign them to envVars
		// Parse the entry by first "=" only, anything after the first "=" is part of the value
		parts := strings.SplitN(e, "=", 2)

		// Split the key by _ and assign the value to the nested map
		keyParts := strings.Split(parts[0], "_")
		l := len(keyParts)
		var current = envVars
		for i, k := range keyParts {
			// Lowercase the key
			k = strings.ToLower(k)

			// Is it the last key?
			if i == l-1 {
				// Check if it's not already type map[string]any
				if _, ok := current[k]; ok {
					break
				}

				current[k] = parts[1]
				break
			}

			// Check envVars[k] is a map, otherwise create it
			if _, ok := current[k]; !ok {
				current[k] = make(map[string]any)
			} else {
				// If it exists but is not a map, convert it to a map
				if _, isMap := current[k].(map[string]any); !isMap {
					current[k] = make(map[string]any)
				}
			}

			// Set envVars[k] to map[string]any
			current = current[k].(map[string]any)
		}
	}

	return envVars
}
