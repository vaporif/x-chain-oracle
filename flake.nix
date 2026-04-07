{
  description = "x-chain-oracle development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    foundry = {
      url = "github:shazow/foundry.nix/monthly";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
    foundry,
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = import nixpkgs {
        inherit system;
        overlays = [foundry.overlay];
      };
      package = pkgs.buildGoModule {
        pname = "x-chain-oracle";
        version = "0.0.1";
        src = self;
        vendorHash = "sha256-JuIqnS2JBUn/ydVk+8KWGnQ38U1GVLNDuAdWORmPNmQ=";
        env.CGO_ENABLED = 1;
      };
    in {
      packages.default = package;
      checks = {
        lint =
          pkgs.runCommand "lint" {
            nativeBuildInputs = with pkgs; [go golangci-lint gcc];
          } ''
            export HOME=$(mktemp -d)
            export GOCACHE=$HOME/go-cache
            export GOPATH=$HOME/gopath
            export CGO_ENABLED=1
            export GOFLAGS="-mod=vendor"
            export GONOSUMCHECK="*"
            export GONOSUMDB="*"
            export GOPROXY=off

            cp -r ${self}/. $HOME/src
            chmod -R u+w $HOME/src

            cp -r ${package.goModules}/. $HOME/src/vendor
            chmod -R u+w $HOME/src/vendor

            cd $HOME/src
            golangci-lint run ./...
            touch $out
          '';

        fmt =
          pkgs.runCommand "fmt" {
            nativeBuildInputs = with pkgs; [go];
          } ''
            export HOME=$(mktemp -d)
            cd ${self}
            test -z "$(gofmt -l .)" || {
              echo "Files not formatted:"
              gofmt -l .
              exit 1
            }
            touch $out
          '';

        typos =
          pkgs.runCommand "typos" {
            nativeBuildInputs = [pkgs.typos];
          } ''
            cd ${self}
            typos
            touch $out
          '';

        nix-fmt =
          pkgs.runCommand "nix-fmt" {
            nativeBuildInputs = [pkgs.alejandra];
          } ''
            alejandra --check ${self}/flake.nix
            touch $out
          '';
      };

      devShells.default = pkgs.mkShell {
        packages = with pkgs; [
          go
          gopls
          gotools
          go-tools
          golangci-lint
          delve
          protobuf
          protoc-gen-go
          protoc-gen-go-grpc
          buf
          just
          typos
          alejandra
          actionlint
          foundry-bin
          solc
        ];

        shellHook = ''
          echo "x-chain-oracle dev shell"
        '';
      };
    });
}
