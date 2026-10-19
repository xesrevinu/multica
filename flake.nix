{
  description = "Multica development environment";

  inputs = {
    nixpkgs.url = "https://flakehub.com/f/DeterminateSystems/nixpkgs-weekly/0.1";
  };

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forEachSystem = nixpkgs.lib.genAttrs systems;
    in
    {
      devShells = forEachSystem (
        system:
        let
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
          };
        in
        {
          default = pkgs.mkShellNoCC {
            packages = [
              pkgs.nodejs_22
              pkgs.pnpm_10
              pkgs.go_1_26
            ];

            shellHook = ''
              export NIX_MULTICA_DEV_SHELL=1
              echo "Multica dev shell: Node $(node --version), pnpm $(pnpm --version), Go $(go version | awk '{print $3}')"
            '';
          };
        }
      );
    };
}
