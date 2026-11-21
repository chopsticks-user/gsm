{
  description = "gsm - CLI for managing self-hosted game servers";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
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
        desktopPath = "$HOME/.local/share/applications";
      };

      # gsm built with a given pkgs, so the overlay uses the consumer's nixpkgs.
      version = self.shortRev or self.dirtyShortRev or "dev";
      gsmFor =
        pkgs:
        pkgs.buildGoModule {
          pname = "gsm";
          inherit version;
          src = self;
          # Recomputed by scripts/update-vendorhash.sh when go.mod or go.sum change.
          vendorHash = "sha256-X1Jc68JkeAxNBY3aM8ByCd3zjIlTRMVrU2QA1N6noHM=";
          subPackages = [ "cmd/gsm" ];
          env.CGO_ENABLED = "0";
          ldflags = [
            "-s"
            "-w"
            "-X main.version=${version}"
          ];
          # The tests need git and real processes; verify runs them, not the sandbox.
          doCheck = false;
          meta = {
            description = "CLI for managing self-hosted game servers";
            homepage = "https://github.com/chopsticks-user/gsm";
            license = pkgs.lib.licenses.mit;
            mainProgram = "gsm";
          };
        };
    in
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        packages.default = gsmFor pkgs;

        devShells.default = pkgs.mkShell {
          # git, steamcmd, steam-run and podman come from the host, not the shell
          packages = with pkgs; [
            go
            gopls
            gcc # go test -race needs cgo
            govulncheck # run in CI; needs the network for the vulnerability database
            just # runs the recipes in justfile
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
                  "$PROJECT_PATH"; # git-aware: copies tracked files only, not .go/
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
    )
    // {
      overlays.default = final: _prev: { gsm = gsmFor final; };
    };
}
