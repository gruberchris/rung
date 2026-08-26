# Contributing to rung

Thanks for your interest. Issues and pull requests are both welcome.

## Getting started

```sh
git clone https://github.com/gruberchris/rung.git
cd rung
go build ./...
go test ./...
```

The unit tests need nothing but Go. They cover file parsing, the dialect
registry, rendering and the CLI's configuration handling.

## Running the integration tests

The tests that exercise `Up`, `Down`, `Statuses` and `reset` run against real
databases, and skip themselves when the corresponding DSN is not set. To run
them all:

```sh
make db-up          # starts PostgreSQL, MySQL and MariaDB in Docker
make test-integration
make db-down
```

Or by hand:

```sh
RUNG_TEST_POSTGRES_DSN="postgres://rung:rung@127.0.0.1:55432/rung?sslmode=disable" \
RUNG_TEST_MYSQL_DSN="rung:rung@tcp(127.0.0.1:53306)/rung" \
RUNG_TEST_MARIADB_DSN="rung:rung@tcp(127.0.0.1:53307)/rung" \
go test -race ./...
```

**These tests drop every table in the database they connect to.** Point them at
a throwaway one.

CI runs all three engines on every pull request, so a change that works on
PostgreSQL but not MariaDB is caught there if not before.

## Before opening a pull request

```sh
make check    # fmt, vet, lint and test
```

Or individually:

```sh
gofmt -l .
go vet ./...
golangci-lint run
go test -race ./...
```

CI also verifies that `go mod tidy` produces no diff.

## Style

The codebase follows ordinary Go style, with one local convention worth
stating: **comments explain why, not what.** A comment that restates the code
is noise; a comment recording the failure a piece of code exists to prevent is
the most valuable thing in the file. Several of the sharper edges here —
`/dev/null` being a character device, MySQL's implicit DDL commits, reads that
must not create the ledger — are documented at the point they matter, and that
is deliberate.

If you fix a bug, the comment should say what broke.

## Adding a dialect

1. Create `dialect/<name>/`, implement `rung.Dialect`, and register it in an
   `init` function.
2. Implement `reset.Resetter` too if `init` should work for it.
3. Add unit tests for the DSN handling and the ledger statements.
4. Add the engine to `engines` in `integration_test.go` and to the service
   containers in `.github/workflows/ci.yml`.

The root package must keep importing only the standard library. A driver
belongs to its dialect subpackage so that nobody pays for a database they do not
use.

## Commit messages

Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`).
The release changelog is generated from them, and `feat:` and `fix:` are the two
that appear in it.

## Releases

Maintainers cut a release by pushing a semver tag:

```sh
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

That triggers the release workflow, which re-runs the tests, builds the CLI for
Linux, macOS and Windows on amd64 and arm64, and publishes a GitHub release with
checksums and a generated changelog. Nothing invents a version number: the tag
is the decision, and it is also what `go get` resolves.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md).
