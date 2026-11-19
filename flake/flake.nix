{
  description = "gsm - CLI for managing self-hosted game servers";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      ...
    }:
    let
      meta = {
        name = "gsm";
        ide = {
          name = "VS Code";
          exec = "code";
          icon = "vscode";
        };
        flakeDir = "flake";
        desktopPath = "$HOME/.local/share/applications";
      };
    in
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          # git, steamcmd, steam-run and podman come from the host, not the shell
          packages = with pkgs; [
            go
            gopls
            gcc # go test -race needs cgo
          ];

          # keep Go state in the repo instead of ~/go and ~/.cache/go-build
          shellHook = ''
            if root=$(git rev-parse --show-toplevel 2>/dev/null); then
              export GOPATH=$root/.go
              export GOMODCACHE=$root/.go/pkg/mod
              export GOCACHE=$root/.go/cache
              export GOFLAGS=-modcacherw # so rm -rf .go and git clean work
            else
              echo "gsm dev shell: not inside a git repo, Go state stays in ~/go" >&2
            fi
          '';
        };
        apps =
          let
            desktopEntry = "${meta.desktopPath}/${meta.ide.exec}-${meta.name}.desktop";
          in
          {
            install =
              let
                developCmdArgumentPath =
                  "path:$PROJECT_PATH" + nixpkgs.lib.optionalString (meta.flakeDir != "") "/${meta.flakeDir}";
              in
              {
                type = "app";
                program = toString (
                  pkgs.writeShellScript "install" ''
                    PROJECT_PATH=$(realpath "''${1:-$(git rev-parse --show-toplevel)}")
                    mkdir -p "${meta.desktopPath}"
                    cat > "${desktopEntry}" <<DESKTOP
                    [Desktop Entry]
                    Name=${meta.ide.name} (${meta.name})
                    Exec=nix develop "${developCmdArgumentPath}" -c ${meta.ide.exec} "$PROJECT_PATH"
                    Path=$PROJECT_PATH
                    Icon=${meta.ide.icon}
                    Type=Application
                    Categories=Development;
                    DESKTOP
                  ''
                );
              };
            uninstall = {
              type = "app";
              program = toString (
                pkgs.writeShellScript "uninstall" ''
                  rm -f "${desktopEntry}"
                ''
              );
            };
          };
      }
    );
}
