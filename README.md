# gsm

[![CI](https://github.com/chopsticks-user/gsm/actions/workflows/ci.yml/badge.svg)](https://github.com/chopsticks-user/gsm/actions/workflows/ci.yml)

gsm is a command-line tool for running self-hosted game servers on your own machine: several named servers per game, each with its own config, saves and port, with config history and save snapshots built in. It's one binary for Linux, Windows and macOS, with no daemon or service to set up.

```
$ gsm create palworld main
created main (Palworld, port 8211)
$ gsm start main
main: started (pid 39692, port 8211, config 681f6e5)
$ gsm save snapshot main -m "before boss fight"
main: snapshot 3735cbf0 (4 files, 33.4 KiB)
$ gsm stop main
main: stopped
main: snapshot 07c32289 (4 files, 33.4 KiB)
```

## Features

- **Several servers per game,** managed by name, with ports assigned so they don't clash.
- **Your data stays safe.** Config and saves live outside the server install and are linked in, so reinstalling or updating a server can't touch them.
- **Config history:** each server's config is a git repo, committed automatically on every start.
- **Save snapshots,** deduplicated and compressed, taken automatically on stop, before updates and before restores. Restores are verified and can always be undone.
- **No daemon:** servers run detached, and gsm finds them again by PID, start time and instance, never by process name.
- **Addons** started and stopped with a server, such as a [playit.gg](https://playit.gg) tunnel.

## Status

gsm is pre-1.0. It supports **Palworld** dedicated servers on Linux and Windows (the Windows code hasn't run on real Windows yet). Enshrouded comes next, in v0.2.0. Off-machine backups and recovery are designed but not built; see the [roadmap](docs/roadmap.md).

## Install

Download the binary for your OS and CPU from the [releases page](https://github.com/chopsticks-user/gsm/releases), check it against `SHA256SUMS`, and put it on your `PATH`:

```
sha256sum --ignore-missing -c SHA256SUMS
install -D -m 755 gsm-linux-amd64 ~/.local/bin/gsm
gsm version
```

gsm needs `git` and `steamcmd` on the machine (and `steam-run` on NixOS); `gsm doctor` checks.

To build from source, see [docs/development.md](docs/development.md).

## Documentation

- [Guide](docs/guide.md): a walkthrough from install to restore, and the command reference
- [Architecture](docs/architecture.md): how gsm works underneath
- [Design](docs/design.md): why it works that way, planned features and known limitations
- [Roadmap](docs/roadmap.md): the releases ahead and what each one delivers
- [Development](docs/development.md): building, testing and releasing
- [Changelog](CHANGELOG.md)

## Contributing

Bug reports, ideas and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). Everyone taking part follows the [Code of Conduct](CODE_OF_CONDUCT.md). To report a security problem, see [SECURITY.md](SECURITY.md).

## License

gsm is released under the [MIT License](LICENSE).
