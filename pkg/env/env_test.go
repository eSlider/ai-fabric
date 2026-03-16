package env

import (
	"os"
	"testing"
)

func TestUnmarschalEnvionment(t *testing.T) {
	tests := []struct {
		name        string
		envVars     map[string]string
		expectedErr bool
		decoded     any
	}{
		{
			name: "Multiple environment variables",
			envVars: map[string]string{
				"VAR_TWO_TREE": "v=alue2=",
				"VAR_ONE":      "value1",
				"VAR_TWO":      "value2",
			},
			expectedErr: false,
			decoded: struct {
				Var struct {
					One string
					Two struct {
						Tree string
					}
				}
			}{
				Var: struct {
					One string
					Two struct {
						Tree string
					}
				}{
					Two: struct {
						Tree string
					}{
						Tree: "v=alue2=",
					},
				},
			},
		},
		{
			name: "Environment variables with empty values",
			envVars: map[string]string{
				"EMPTY_VAR": "",
			},
			expectedErr: false,
		},
		{
			name:        "No environment variables",
			envVars:     map[string]string{},
			expectedErr: false,
		},
		{
			name: "Single environment variable",
			envVars: map[string]string{
				"TEST_VAR": "value",
			},
			expectedErr: false,
		},

		{
			name: "Environment variable with special characters",
			envVars: map[string]string{
				"SPECIAL_VAR": "@!#$%^&*()",
			},
			expectedErr: false,
			decoded: struct {
				SpecialVar string
			}{
				SpecialVar: "@!#$%^&*()",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			err := UnmarshalEnvironment(&tt.decoded)
			if (err != nil) != tt.expectedErr {
				t.Errorf("expected error: %v, got error: %v", tt.expectedErr, err != nil)
			}

			os.Clearenv()
		})
	}
}
