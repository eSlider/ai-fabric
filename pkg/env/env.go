package env

import (
	"os"
	"reflect"
	"strings"

	"github.com/mitchellh/mapstructure"
)

// UnmarshalEnvironment reads all environment variables and unmarshals them into s.
func UnmarshalEnvironment(s any) error {
	asMap := GetEnvVarsAsMap()
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           s,
		WeaklyTypedInput: true,
		DecodeHook:       mapstructure.ComposeDecodeHookFunc(trimStringHook),
	})
	if err != nil {
		return err
	}
	return decoder.Decode(asMap)
}

func trimStringHook(from reflect.Type, to reflect.Type, data interface{}) (interface{}, error) {
	if from.Kind() == reflect.String && to.Kind() == reflect.String {
		return strings.TrimSpace(data.(string)), nil
	}
	return data, nil
}

func GetEnvVarsAsMap() map[string]any {
	var envVars = make(map[string]any)

	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		keyParts := strings.Split(parts[0], "_")
		l := len(keyParts)
		var current = envVars
		for i, k := range keyParts {
			k = strings.ToLower(k)

			if i == l-1 {
				if _, ok := current[k]; ok {
					break
				}
				current[k] = parts[1]
				break
			}

			if _, ok := current[k]; !ok {
				current[k] = make(map[string]any)
			} else {
				if _, isMap := current[k].(map[string]any); !isMap {
					current[k] = make(map[string]any)
				}
			}

			current = current[k].(map[string]any)
		}
	}

	return envVars
}
