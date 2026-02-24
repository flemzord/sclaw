{ pkgs ? import <nixpkgs> {} }:

let
  lint = pkgs.writeShellScriptBin "lint" ''
    golangci-lint run
  '';

  run-tests = pkgs.writeShellScriptBin "run-tests" ''
    go test -race -coverprofile=coverage.txt ./...
  '';
in
pkgs.mkShell {
  buildInputs = with pkgs; [
    go
    golangci-lint
    goreleaser
    lint
    run-tests
  ];

  shellHook = ''
    export GOPATH="$HOME/go"
    export PATH="$GOPATH/bin:$PATH"
  '';
}
