---
name: api-client-design
description: Use when implementing, reviewing, or refactoring ANY API/HTTP client (REST, SDK wrapper, vendor API) in any language. Encodes the canonical client architecture from the go-onlyoffice reference implementation — client lifecycle, auth token management, transport layering, envelope decoding, error handling, and test policy.
---

# API Client Design

Canonical first-class example: **`go-onlyoffice`** (`path/to/go-onlyoffice`,
`github.com/eSlider/go-onlyoffice`). Read `client.go`, `auth.go`, `http.go`, `request.go`
and its `AGENTS.md` before designing a new client or judging an existing one.

## Architecture (mandatory layers)

Split every client into four concerns, each in its own file/module:

1. **Client core** (`client.go`) — `Client` struct holding the `*http.Client` (or language
   equivalent), credentials, cached session/token state, and optional defaults.
   - Constructor `NewClient(creds)` with sane defaults; document concurrency guarantees
     ("safe for sequential use from one goroutine") on the struct.
   - Credentials come from an explicit struct; provide a separate env-var reader
     (`GetEnvironmentCredentials()`) with documented variable names + aliases.
2. **Auth** (`auth.go`) — all token lifecycle in one place:
   - Lazy auth: fetch token on first request (`ensureToken`), not at construction.
   - Validity check (`tokenValid`) using the server-provided expiry.
   - Eager entry points: `Authenticate()` and context-aware `AuthenticateContext(ctx)`
     for long-running jobs (auth honours cancellation/deadlines).
   - `InvalidateToken()` to recover from mid-session 401 revocation while local expiry
     still looks fresh.
3. **Transport** (`http.go`) — thin, DRY request primitives sharing `*Client` state so
   base URL, token refresh, and caching are handled exactly once:
   - One helper per content type / verb family (`getJSON`, `postJSON`, `postForm`,
     `uploadMultipart`), each: build request with ctx → set headers → do → read body →
     check status → return raw payload.
   - Envelope decoders on top of raw helpers (`ResponseArray`, `ResponseObject`) so no
     domain method ever hand-rolls `json.Unmarshal(envelope...)` blocks. Handle JSON
     null / empty-list payloads explicitly (return nil, nil).
4. **Domain methods** (one file per subject: `projects.go`, `crm.go`, …) — typed methods
   on `*Client` calling the transport helpers. Split by file, NOT by subpackage, so all
   call sites are uniform `c.XxxYyy()` against a single handle.

## Hard rules

- **Context everywhere**: every request primitive takes `ctx` and uses
  `NewRequestWithContext`. Never block past the caller's deadline.
- **Errors**: any status >= 400 is an error containing method/path/status code plus a
  TRUNCATED response body (~400 chars) — never dump full HTML payloads into errors.
  Wrap with `%w`; never swallow.
- **Library purity**: the library never loads `.env`/dotenv/config files — that is the
  CLI's job. No UI deps in the library; CLI-only deps stay out of the library module.
- **No secrets** committed; `.env.example` only.
- **Body normalisation**: nil → `{}` for JSON posts; `[]byte`/`string` pass through;
  structs marshal to JSON. Query params via struct tags + query-string library, never
  string concatenation of user input.
- **Wire-format quirks** (odd date formats, envelope shapes) get dedicated types with
  `Marshal`/`Unmarshal` implementations in ONE place, not per call site.
- Prefer reusing existing transport helpers over adding new ones; if you copy-paste a
  decode block, factor it out instead.

## Configuration & environment (go-config philosophy)

Reference: [`github.com/eSlider/go-config`](https://github.com/eSlider/go-config)
(converts **env / YAML / JSON / TOML / INI** ↔ Go structs via `map[string]any`).

- **Config is a merge of layered sources**, ordered lowest → highest priority:
  shipped defaults → file (`.default.env`, `.env`) → process environment.
  Later sources override earlier ones — the process env always wins.
- **Deep map merge**: nested maps combine recursively; scalar leaves are
  last-write-wins; slices default to replace (opt-in concat).
- **Keys are normalized** with a lower+alnum rule so `sub-service`,
  `SUB_SERVICE`, and `SubService` line up across formats
  (`Service.SubService.Name` ⇄ `service.sub-service.name` ⇄ `SERVICE_SUBSERVICE_NAME`).
- Never hand-roll per-format parsing or ad-hoc `os.Getenv` ladders in client
  code: declare a config struct and let a codec layer (e.g. go-config) load,
  normalize, and merge into it. The library stays dotenv-free; binaries wire
  their own sources.

## Test policy (no synthetic vendor mocks)

- **Unit tests**: pure logic only — parsers, encoders, struct conversions. No network,
  NO fake servers emulating the vendor (`httptest.NewServer` mocking the vendor API is
  forbidden).
- **Integration tests** behind a build tag (`//go:build integration`), hitting a live
  instance; credentials from documented env vars; tests **skip cleanly** when creds are
  missing so plain `go test ./...` stays green in CI.
- Every new endpoint ships with an integration test before merge.

## Checklist before merging a new client

- [ ] Four-layer split present (core / auth / transport / domain-by-file)
- [ ] Token lazy + invalidation + context-aware auth implemented
- [ ] All requests take ctx; statuses >= 400 produce informative truncated errors
- [ ] Envelope decoding centralised; zero duplicated decode blocks
- [ ] Env credential reader documented; library does no dotenv loading
- [ ] Unit tests pure; integration tests tagged + skipping without creds
