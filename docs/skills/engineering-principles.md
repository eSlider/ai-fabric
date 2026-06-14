# Engineering Principles (inviolable)

These rules apply to every change in this repository — human or agent.
The architect designs within them, the developer implements within them,
the reviewer rejects violations. They exist so the fabric can grow into a
multi-project assistant without collapsing under its own code.

Operationalized by `docs/skills/lazy-senior-dev.md` (YAGNI ladder, ponytail
comments on intentional shortcuts). When in doubt: delete, don't abstract.

## 1. Reuse first, no busywork

- Before writing any code, search for an existing tool, library, SDK, or
  in-repo package that already does it. Re-implementing what exists is a
  rejected-review offense.
- Before defining a struct that mirrors an external system, check whether the
  system's own Go SDK/source already exports it. Gitea is written in Go —
  use `code.gitea.io/sdk/gitea` types (`sdk.Issue`, `sdk.PullRequest`, ...).
  The same applies to future integrations (Matrix, etc.).
- Every change must trace to a concrete goal (an issue or the architect's
  plan). No work for work's sake, no drive-by rewrites, no decorative
  refactors.
- State in the PR what existing code/libraries were reused.

## 2. Occam's razor / YAGNI

- The smallest design that satisfies the issue wins.
- No speculative abstractions; no new layer without a second concrete
  consumer.
- Delete code rather than keep it "just in case".

## 3. Idiomatic Go

- Standard library first; dependencies must earn their place.
- Small interfaces defined at the consumer; accept interfaces, return structs.
- Wrap errors with context: `fmt.Errorf("context: %w", err)`. No panics in
  normal control flow.
- Pure functions where possible; no hidden mutations of inputs.
- Stay on the current Go release and keep code modern: run `go fix ./cmd/... ./pkg/... ./internal/...`
  (Go 1.26 modernizers) after upgrades; prefer current idioms (`any`,
  `slices`/`maps` helpers, `strings.SplitSeq`, `new(expr)`).

## 4. No duplication (rule of three)

- A second copy of a fragment is a smell; a third is forbidden.
- Repeated fragments move into `pkg/` packages (or a shared library repo once
  multi-project, see `docs/architecture/multi-project-topology.md`).
- Repeated struct-to-struct field copying is duplication: decode once at the
  boundary into a typed struct. For genuinely map-shaped data use
  `github.com/go-viper/mapstructure/v2`.

## 5. 3-tier structure (not DDD, not flat)

- `cmd/` — entrypoints and transport (CLI flags, HTTP servers, Telegram).
- `pkg/fabric` and peers — orchestration / use cases.
- `pkg/gitea`, `internal/config`, `pkg/file` — integration and data access.
- Tiers only call downward. Entities (typed domain structs, preferably
  upstream SDK types) are shared across tiers; logic is not.

## 6. Testing: no mocks, no isolated unit tests

- Mock-based and narrow unit tests camouflage problems and are forbidden.
  A test that mocks a collaborator is a bug.
- Allowed test categories:
  - **Use-case tests** — drive a real flow end-to-end (real Gitea instance,
    real processes; an agent binary may be substituted by a deterministic
    script — a real process, not a Go mock).
  - **API tests** — real HTTP against real servers (`httptest` serving real
    handlers counts; hand-rolled fakes of our own interfaces do not).
  - **System tests** — full compose stack, tagged `//go:build system`.
- Behavior-first: write the use-case test against the real stack before the
  implementation.

## 7. Configuration and exchange formats: YAML over JSON

- Configuration files and human-authored or inter-service exchange formats
  must be YAML (`gopkg.in/yaml.v3` is already a dependency). JSON is
  needlessly verbose for these.
- JSON stays only where the protocol dictates it (Gitea REST API, webhook
  payloads).

## 8. Operational safety

- No destructive git actions in automation flows.
- Automation must be idempotent and loop-safe: every automated reaction to an
  automated event needs an explicit budget and a terminal state.
- Secrets never enter code, prompts, logs, or process argument lists.
