# Security policy

## Supported versions

Security fixes go into the latest release only. gsm is pre-1.0, so please upgrade to the newest version before reporting.

## Reporting a vulnerability

Please don't open a public issue for a security problem. Report it privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability**. Include what you found, how to reproduce it, the gsm version (`gsm version`) and your OS.

You'll get a reply as soon as the maintainer can look at it. Once a fix is released, the advisory is published with credit to you, unless you'd rather stay anonymous.

## Scope

gsm runs game servers you install and manages files under its data directory. Reports about the games themselves, steamcmd or playit.gg belong with those projects. Things that are in scope include gsm acting on files outside its data directory, mixing up two instances' data, exposing secrets (such as the playit env file) or running something other than the configured server.
