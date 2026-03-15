# End-to-End PoC Validation

## Preconditions

1. `cp .env.example .env`
2. Gitea runner registration token set in `.env`
3. `./bin/up.sh`

## Flow: issue -> branch -> PR -> checks

1. Create an issue in Gitea repository.
2. Create branch `feature/<short-topic>`.
3. Implement change with tests-first approach.
4. Open PR in Gitea.
5. Gitea Actions runs:
   - `bin/fmt.sh`
   - `bin/lint.sh`
   - `bin/test.sh`
   - `bin/review_policy.sh`
6. Review automation reports deterministic policy results.
7. Human reviewer approves and merges only after green checks.

## Local Verification Executed

The following command was executed successfully in this repository:

`./bin/bootstrap.sh && ./bin/fmt.sh && ./bin/lint.sh && ./bin/test.sh && ./bin/review_policy.sh && docker compose -f docker-compose.yml config`
