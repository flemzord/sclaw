{ pkgs ? import <nixpkgs> {} }:

let
  lint = pkgs.writeShellScriptBin "lint" ''
    golangci-lint run
  '';

  test = pkgs.writeShellScriptBin "test" ''
    go test -race -coverprofile=coverage.txt ./...
  '';
in
pkgs.mkShell {
  buildInputs = with pkgs; [
    go
    golangci-lint
    goreleaser
    lint
    test
  ];

  shellHook = ''
    export GOPATH="$HOME/go"
    export PATH="$GOPATH/bin:$PATH"
  '';
}
