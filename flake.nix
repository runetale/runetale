{
  description = "runetale";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let

      # Generate a user-friendly version number.
      version = builtins.substring 0 8 self.lastModifiedDate;

      # System types to support.
      supportedSystems = [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin" ];

      # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Nixpkgs instantiated for supported system types.
      nixpkgsFor = forAllSystems (system: import nixpkgs { inherit system; });

    in
    {
      # Provide some binary packages for selected system types.
      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
        in
        {
          # TOOD: (shinta) allow the runetale command to be built
          runetale = pkgs.buildGoModule {
            pname = "runetale";
            inherit version;
            src = builtins.path {
              path = ./.;
              filter = (path: type: builtins.elem path (builtins.map toString [
                ./go.mod
                ./go.sum
              ]));
            };

            buildPhase = ''
              go build -o runetale cmd/runetale/main.go
            '';
            
            installPhase = ''
              install -Dm755 runetale -t $out/bin
            '';

            vendorSha256 = pkgs.lib.fakeSha256;
            #vendorSha256 = "sha256-X1rUNBIOWZ9aH1X8xrE4wybGqe3llfFiDH84TvwsZps=";
          };

          # build systemd service path.
          # use when you want to generate daemon service files in nix store
          daemon-service = pkgs.substituteAll {
            name = "runetaled.service";
            src = ./systemd/runetaled.service;
          };
        });

        defaultPackage = forAllSystems (system: self.packages.${system}.runetale);

        devShell = forAllSystems (system:
          let pkgs = nixpkgsFor.${system};
          in pkgs.mkShell {
            buildInputs = with pkgs; [
              protoc-gen-go
              gopls
              go_1_22
              protobuf
              protoc-gen-go-grpc
              docker-compose
              docker
              sqlite
              wireguard-tools
            ];

            shellHook = ''
              export GOPATH=$GOPATH
              PATH=$PATH:$GOPATH/bin
              export GO111MODULE=on
            '';
          });
    };
}
