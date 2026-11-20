# Contributing to gsm

Thanks for your interest in gsm. Bug reports, ideas and pull requests are all welcome. Everyone taking part is expected to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Reporting a bug

Open an issue with the **Bug report** form. The most useful reports include the gsm version (`gsm version`), your OS, the game, the exact commands you ran and what happened, including the relevant part of `gsm logs <name>`. Check the [known limitations](docs/design.md#known-limitations) and existing issues first.

Security problems go through private reporting instead; see [SECURITY.md](SECURITY.md).

## Proposing a feature or asking a question

Open an issue with the **Feature or question** form. For a new game, say which one, whether its dedicated server is on Steam (and its app ID if you know it), and which OSes it ships a server for. The [roadmap](docs/design.md#roadmap) lists what's planned.

## Pull requests

1. For anything beyond a small fix, open or comment on an issue first, so the approach can be agreed before you write code.
2. Read [docs/development.md](docs/development.md): it covers the dev environment, the verify command, how tests are written, and the branch and pull request workflow. [docs/architecture.md](docs/architecture.md) explains how the code fits together.
3. Keep the change focused, with tests for new behavior, and update the docs it affects in the same pull request.
4. Run verify before pushing: `nix develop path:./flake -c scripts/verify.sh`.
5. Open the pull request against `main` and fill in the template. CI runs verify and a vulnerability scan on it.

Adding a game is described in [docs/development.md](docs/development.md#adding-a-game).

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
