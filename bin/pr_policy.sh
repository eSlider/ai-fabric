#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ "${GITHUB_EVENT_NAME:-}" != "pull_request" ]]; then
  echo "pr policy skipped: event is '${GITHUB_EVENT_NAME:-unknown}'"
  exit 0
fi

if [[ -z "${GITHUB_EVENT_PATH:-}" || ! -f "${GITHUB_EVENT_PATH}" ]]; then
  echo "::error::GITHUB_EVENT_PATH is missing for pull_request event"
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "::error::go command is required for PR policy checks"
  exit 1
fi

go_file="$(mktemp "${TMPDIR:-/tmp}/pr-policy-XXXXXX.go")"
trap 'rm -f "${go_file}"' EXIT

cat >"${go_file}" <<'GO'
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func asMap(value any) map[string]any {
	m, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

func asString(value any) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return s
}

func asInt(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	default:
		return 0
	}
}

func fetchPRBody(payload map[string]any) string {
	prObj := asMap(payload["pull_request"])
	number := asInt(prObj["number"])
	if number == 0 {
		number = asInt(payload["number"])
	}

	repo := asMap(payload["repository"])
	fullName := asString(repo["full_name"])
	if fullName == "" {
		fullName = os.Getenv("GITHUB_REPOSITORY")
	}

	token := os.Getenv("GITHUB_TOKEN")
	server := strings.TrimRight(os.Getenv("GITHUB_SERVER_URL"), "/")
	if number == 0 || fullName == "" || token == "" || server == "" {
		return ""
	}

	url := fmt.Sprintf("%s/api/v1/repos/%s/pulls/%d", server, fullName, number)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "token "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var prResp map[string]any
	if err := json.Unmarshal(data, &prResp); err != nil {
		return ""
	}

	return strings.TrimSpace(asString(prResp["body"]))
}

func main() {
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	raw, err := os.ReadFile(eventPath)
	if err != nil {
		fmt.Printf("::error::failed reading event payload: %v\n", err)
		os.Exit(1)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		fmt.Printf("::error::failed parsing event payload JSON: %v\n", err)
		os.Exit(1)
	}

	pr := asMap(payload["pull_request"])
	body := strings.TrimSpace(asString(pr["body"]))
	if body == "" {
		body = fetchPRBody(payload)
	}

	requiredSections := []string{
		"## Problem",
		"## Solution",
		"## Risks",
		"## Test Plan",
		"## Rollback",
		"## Issue Link",
		"## AI Notes",
	}

	hasErrors := false
	for _, section := range requiredSections {
		if !strings.Contains(body, section) {
			fmt.Printf("::error::PR template section missing: %s\n", section)
			hasErrors = true
		}
	}
	if hasErrors {
		os.Exit(1)
	}

	issueRefRe := regexp.MustCompile(`(?im)\b(closes|fixes|refs)\s+#\d+\b`)
	fallbackRefRe := regexp.MustCompile(`(?m)#\d+\b`)
	if !issueRefRe.MatchString(body) && !fallbackRefRe.MatchString(body) {
		fmt.Println("::warning::Issue Link reference not detected in PR body")
	}

	fmt.Println("pr policy check passed.")
}
GO

go run "${go_file}"
