# Contributing to gsm

Thanks for your interest in gsm. Bug reports, ideas and pull requests are all welcome. Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting a bug

Open an issue with the **Bug report** form, which asks for what's needed to reproduce it. Check the [known limitations](docs/design.md#known-limitations) and existing issues first.

Security problems go through private reporting instead; see [SECURITY.md](SECURITY.md).

## Proposing a feature or asking a question

Open an issue with the **Feature or question** form. For a new game, say which one, whether its dedicated server is on Steam (and its app ID if you know it), and which OSes it ships a server for. The [roadmap](docs/design.md#roadmap) lists what's planned.

## Pull requests

1. For anything beyond a small fix, open or comment on an issue first, so the approach can be agreed before you write code.
2. Read [docs/development.md](docs/development.md): it covers the dev environment, the verify command, how tests are written, and the branch and pull request workflow. [docs/architecture.md](docs/architecture.md) explains how the code fits together.
3. Follow the workflow there: a focused change with tests and the docs it affects, verify passing, and a pull request that fills in the template. CI checks it too.

Adding a game is described in [docs/development.md](docs/development.md#adding-a-game).

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
