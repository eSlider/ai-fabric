# Backup and Restore

`var/gitea/` is the single source of truth for the whole fabric: the SQLite
database, all git repositories, Actions logs and runner registrations. Losing
it loses the SCM and CI control plane.

## Backup

```bash
./bin/backup.sh            # writes var/backups/gitea-<UTC timestamp>.tar.gz
./bin/backup.sh <backup-dir>   # custom output directory
```

The script stops the `gitea` container for the duration of the tar (seconds to
minutes) to get a consistent SQLite snapshot, then starts it again. The last 7
archives are kept; older ones are rotated out.

Schedule it via cron on the host, e.g. daily at 04:00:

```cron
0 4 * * * cd /path/to/ai-fabric && ./bin/backup.sh >> var/backups/backup.log 2>&1
```

Copy archives off-host (NAS, object storage) for real disaster recovery.

## Restore

```bash
docker compose stop gitea
mv var/gitea var/gitea.broken-$(date +%s)   # keep the damaged state aside
tar -xzf var/backups/gitea-<stamp>.tar.gz -C var/
docker compose start gitea
```

After restore verify:

- `curl -fsS http://localhost:3000/api/healthz`
- repositories and issues are visible in the UI
- the Actions runner reconnects (`docker compose logs gitea-runner`)

## What is intentionally not backed up

- `var/agents/` — disposable git worktrees, recreated on demand
- `var/issue-handler/` — no durable state lives there
- runner registration in `var/runner-1/` — re-registering is cheap; back it up
  only if you want to avoid stale runner entries in the Gitea UI

Changing `GITEA_RUNNER_LABELS` requires removing `var/runner-*/.runner` (or
recreating the runner volume) before restart so act_runner re-registers with
the new labels.
