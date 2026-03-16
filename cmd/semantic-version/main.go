package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var semverTagPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
var bangChangePattern = regexp.MustCompile(`^[A-Za-z]+(\([^)]+\))?!:`)
var featPattern = regexp.MustCompile(`^feat(\([^)]+\))?:`)

type semver struct {
	major int
	minor int
	patch int
	tag   string
}

func main() {
	if len(os.Args) != 2 || os.Args[1] != "next" {
		fmt.Fprintln(os.Stderr, "Usage: semantic-version next")
		os.Exit(1)
	}

	version, err := resolveNextVersion()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(version)
}

func resolveNextVersion() (string, error) {
	headTagsRaw, err := runGit("tag", "--points-at", "HEAD")
	if err != nil {
		return "", err
	}
	headTag := latestSemverTag(splitLines(headTagsRaw))
	if headTag != nil {
		return strings.TrimPrefix(headTag.tag, "v"), nil
	}

	allTagsRaw, err := runGit("tag", "--list")
	if err != nil {
		return "", err
	}
	latest := latestSemverTag(splitLines(allTagsRaw))

	var commitsRaw string
	if latest == nil {
		commitsRaw, err = runGit("log", "--format=%B")
	} else {
		commitsRaw, err = runGit("log", "--format=%B", latest.tag+"..HEAD")
	}
	if err != nil {
		return "", err
	}

	base := semver{major: 0, minor: 0, patch: 0}
	if latest != nil {
		base = *latest
	}
	switch requiredBump(commitsRaw) {
	case "major":
		base.major++
		base.minor = 0
		base.patch = 0
	case "minor":
		base.minor++
		base.patch = 0
	default:
		base.patch++
	}
	return fmt.Sprintf("%d.%d.%d", base.major, base.minor, base.patch), nil
}

func latestSemverTag(tags []string) *semver {
	parsed := make([]semver, 0)
	for _, tag := range tags {
		m := semverTagPattern.FindStringSubmatch(strings.TrimSpace(tag))
		if len(m) != 4 {
			continue
		}
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		patch, _ := strconv.Atoi(m[3])
		parsed = append(parsed, semver{major: major, minor: minor, patch: patch, tag: tag})
	}
	if len(parsed) == 0 {
		return nil
	}
	sort.Slice(parsed, func(i, j int) bool {
		if parsed[i].major != parsed[j].major {
			return parsed[i].major < parsed[j].major
		}
		if parsed[i].minor != parsed[j].minor {
			return parsed[i].minor < parsed[j].minor
		}
		return parsed[i].patch < parsed[j].patch
	})
	last := parsed[len(parsed)-1]
	return &last
}

func requiredBump(commitsRaw string) string {
	entries := strings.Split(commitsRaw, "\n\n")
	hasMinor := false
	hasPatch := false
	for _, e := range entries {
		msg := strings.TrimSpace(e)
		if msg == "" {
			continue
		}
		firstLine := strings.Split(msg, "\n")[0]
		if strings.Contains(msg, "BREAKING CHANGE:") || bangChangePattern.MatchString(firstLine) {
			return "major"
		}
		if featPattern.MatchString(firstLine) {
			hasMinor = true
			continue
		}
		hasPatch = true
	}
	if hasMinor {
		return "minor"
	}
	if hasPatch {
		return "patch"
	}
	return "patch"
}

func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func splitLines(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(s), "\n")
}
