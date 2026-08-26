# Security Policy

## Supported versions

`rung` is pre-1.0. Security fixes are made on the latest minor release.

| Version | Supported |
| ------- | --------- |
| latest  | yes       |
| older   | no        |

## Reporting a vulnerability

Please report security issues privately through
[GitHub Security Advisories](https://github.com/gruberchris/rung/security/advisories/new)
rather than by opening a public issue.

Include what the issue is, how to reproduce it, and what an attacker could do
with it. You can expect an acknowledgement within a week.

## Scope

`rung` executes SQL you supply against a database you point it at, using an
account you grant DDL privileges to. Migration files are trusted input by
definition: a migration that drops your tables is doing its job.

What *is* in scope:

- The multi-statement connection leaking into anything other than migration
  execution. `Dialect.OpenForMigrations` deliberately relaxes a driver's
  defence against statement injection, and it must never be the connection an
  application serves traffic with.
- Credentials appearing in output. DSNs are masked before being printed; a path
  that prints one unmasked is a bug.
- The confirmation prompt being bypassable, or a destructive command proceeding
  without either `--force` or an answer from a real terminal.
- Identifier interpolation in `reset`, which cannot be parameterised and is
  therefore quoted.

## A note on privileges

`rung` is designed on the assumption that the account applying migrations is
*not* the account your service runs as. If you have given your application's
database role DDL privileges so that it can migrate itself, a bug in a request
handler can alter your schema. That is a configuration risk this tool tries to
make easy to avoid; see the README.
