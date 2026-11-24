# Contributing

Thanks for taking the time to look at this project. Bug reports, questions and pull requests are all welcome.

## Reporting a bug

Open an issue with:

- what you did (the request, ideally as a `curl` command),
- what you expected,
- what happened instead, including the response body and any server log lines.

The version (`storefront -version`) and your OS help too.

## Setting up

You need Go 1.27 or newer. There's no C toolchain or database to install: SQLite comes in through a pure-Go driver.

```sh
git clone https://github.com/ccchimi/storefront-api.git
cd storefront-api
go test ./...
go run ./cmd/storefront
```

The `Makefile` has shortcuts for the common tasks (`make test`, `make race`, `make cover`, `make vet`, `make fmt`).

## Making a change

1. For anything bigger than a small fix, open an issue first so we can agree on the approach before you spend time on it.
2. Create a branch from `main`.
3. Make the change, with tests.
4. Before pushing, run what CI runs:

   ```sh
   gofmt -l .        # should print nothing
   go vet ./...
   go test -race ./...
   ```

5. Add a line under `[Unreleased]` in [CHANGELOG.md](CHANGELOG.md).
6. Open a pull request describing what changed and why.

## Guidelines

- **Tests come with the change.** Behaviour that matters through the API belongs in `internal/api/api_test.go`, which runs the whole app against a real SQLite file with fake payment and email services. Small, self-contained logic can have unit tests next to it.
- **Keep failure paths honest.** Checkout must stay all-or-nothing, stock must never go negative, and a retried request must never charge twice. If a change touches checkout, add a test for the way it could go wrong, not only the happy path.
- **No package-level state.** Anything with state (caches, limiters, locks) lives on the `App`, so tests can run independent apps side by side.
- **Money is integer cents** (`money.Cents`). Don't introduce floats for amounts.
- **SQL uses bound parameters**, never string concatenation with user input.
- **Errors use the shared format** in `internal/api/respond.go`, and validation errors name each invalid field.
- **Dependencies:** the project sticks to the standard library plus the SQLite driver. Please discuss a new dependency in an issue first.
- **Comments explain why**, not what the next line does.

## Commit messages

Write a short summary line in the imperative ("Add order export", not "Added" or "Adds"), then a blank line and a few lines on the reason for the change if it isn't obvious.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
