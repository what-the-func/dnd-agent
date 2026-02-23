{
  description = "dnd-agent - AI-powered D&D 5e Dungeon Master";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
          config = {
            allowUnfree = true;
            cudaSupport = true;
          };
        };
        isLinux = pkgs.stdenv.isLinux;
        cudaDeps = pkgs.lib.optionals isLinux [
          pkgs.cudaPackages.cuda_cudart
          pkgs.cudaPackages.libcublas
        ];
        vulkanDeps = pkgs.lib.optionals isLinux [
          pkgs.vulkan-loader
          pkgs.vulkan-headers
        ];
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs =
            with pkgs;
            [
              go
              libffi
              stdenv.cc.cc.lib
            ]
            ++ cudaDeps
            ++ vulkanDeps;

          env = {
            LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath (
              [
                pkgs.libffi
                pkgs.stdenv.cc.cc.lib
              ]
              ++ cudaDeps
              ++ vulkanDeps
            );
          };
        };
      }
    );
}
