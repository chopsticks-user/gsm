# Examples

Each game's default config, as the game ships or first writes it, under the file name gsm uses in an instance's `config/`. They are for reading: gsm doesn't use these files. How each game's config gets into `config/` is in the [guide](../docs/guide.md#step-3-create-an-instance) (Palworld) and its [Enshrouded section](../docs/guide.md#enshrouded).

| File | Where it comes from | Server build |
|---|---|---|
| [palworld/PalWorldSettings.ini](palworld/PalWorldSettings.ini) | `DefaultPalWorldSettings.ini` in the install, which `create` copies into `config/` | 25247047 |
| [enshrouded/enshrouded_server.json](enshrouded/enshrouded_server.json) | written by the server on its first start | 26430253 |

Game updates can change the defaults; these files are not updated with them. Generated secrets are replaced with placeholders: Enshrouded's first start writes a random password for each user group, shown here as `<random, generated on first start>`.

The Palworld file keeps the game's own header, which says changes to it are not used. That is true of `DefaultPalWorldSettings.ini` in the install; the copy in an instance's `config/PalWorldSettings.ini` is the one the server reads.
