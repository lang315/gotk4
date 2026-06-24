{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    # Pinned to a nixos-unstable rev providing GTK 4.22.4. Bump this rev to
    # change the GTK version the bindings are generated against.
    nixpkgs-gotk4.url = "github:NixOS/nixpkgs/567a49d1913ce81ac6e9582e3553dd90a955875f";
    flake-utils.url = "github:numtide/flake-utils";
    flake-compat.url = "https://flakehub.com/f/edolstra/flake-compat/1.tar.gz";

    gotk4-nix.url = "github:diamondburned/gotk4-nix";
    gotk4-nix.inputs = {
      nixpkgs.follows = "nixpkgs";
      flake-utils.follows = "flake-utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-gotk4,
      gotk4-nix,
      flake-utils,
      flake-compat,
    }:

    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = gotk4-nix.lib.mkShell {
          base.pname = "gotk4";
          pkgs = import nixpkgs-gotk4 {
            inherit system;
            overlays = [
              # gotk4-nix.overlays.patchedGo
              gotk4-nix.overlays.patchelf
              # Compat shim: nixpkgs renamed wrapGAppsHook -> wrapGAppsHook3, but
              # the pinned gotk4-nix still references the old name.
              (final: prev: { wrapGAppsHook = prev.wrapGAppsHook3; })
            ];
          };
          go = pkgs.go_1_24;
          inherit (pkgs) gopls gotools;
        };
        packages.dockerEnv = pkgs.buildEnv {
          name = "gotk4-docker-env";
          paths = with pkgs; [
            stdenv.cc
            stdenv.shellPackage
            (pkgs.writeShellScriptBin "docker-env" ''
              set -e

              cmd=$1
              shift

              case "$cmd" in
              "init")
                ${pkgs.nix}/bin/nix-shell --pure --run 'declare -xp > /nix-environment'
                ;;
              "exec")
                __user=$USER
                __home=$HOME

                source /nix-environment

                USER=$__user
                HOME=$__home

                eval "$@"
                ;;
              esac
            '')
          ];
        };
      }
    );
}
