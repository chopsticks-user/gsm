# Examples

Each game's default config, as the game ships or first writes it, under the file name gsm uses in an instance's `config/`. They are for reading: gsm doesn't use these files, and `create` still takes the config from the install ([guide](../docs/guide.md#step-3-create-an-instance)).

| File | Where it comes from | Server build |
|---|---|---|
| [palworld/PalWorldSettings.ini](palworld/PalWorldSettings.ini) | `DefaultPalWorldSettings.ini` in the install, which `create` copies into `config/` | 25247047 |
| [enshrouded/enshrouded_server.json](enshrouded/enshrouded_server.json) | written by the server on its first start | 26430253 |

Game updates can change the defaults; these files are not updated with them. Generated secrets are replaced with placeholders: Enshrouded's first start writes a random password for each user group, shown here as `<random, generated on first start>`. `queryPort` is the game's default, which gsm overwrites with the instance's port before every start.
