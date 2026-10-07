{
  description = "memstore — a portable memory store";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs = { nixpkgs, ... }:
    let
      systems = [ "aarch64-darwin" "x86_64-darwin" "aarch64-linux" "x86_64-linux" ];
      eachSystem = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: nixpkgs.legacyPackages.${system};
    in {
      devShells = eachSystem (system:
        let pkgs = pkgsFor system;
        in {
          default = pkgs.mkShell {
            packages = [ pkgs.go_1_27 pkgs.gopls ];
          };
        });

      packages = eachSystem (system:
        let pkgs = pkgsFor system;
        in {
          default = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
            pname = "memstore";
            version = "0.1.0";
            src = ./.;
            vendorHash = null;          # vendored, so no hash and no fetch
            subPackages = [ "cmd/memstore" ];
          };
        });
    };
}
