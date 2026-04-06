{
  description = "x-chain-oracle development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
      package = pkgs.buildGoModule {
        pname = "x-chain-oracle";
        version = "0.0.1";
        src = self;
        vendorHash = "sha256-9nrZEmtR2gKnRGDbBdYgfwQftHVP2Hdl4ZOxjVCtLq8=";
        env.CGO_ENABLED = 1;
      };
    in {
      checks = {
        lint =
          pkgs.runCommand "lint" {
            nativeBuildInputs = with pkgs; [go golangci-lint gcc];
          } ''
            export HOME=$(mktemp -d)
            export GOCACHE=$HOME/go-cache
            export GOMODCACHE=$HOME/gomod
            export GOPATH=$HOME/gopath
            export CGO_ENABLED=1

            cp -r ${self}/. $HOME/src
            chmod -R u+w $HOME/src

            cp -r ${package.goModules}/. $HOME/gomod
            chmod -R u+w $HOME/gomod

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
      };

      devShells.default = pkgs.mkShell {
        packages = with pkgs; [
          go
          gopls
          gotools
          go-tools
          golangci-lint
          protobuf
          protoc-gen-go
          protoc-gen-go-grpc
          buf
        ];

        shellHook = ''
          echo "x-chain-oracle dev shell"
        '';
      };
    });
}
