package bootstrap

import "testing"

func TestBootstrapper_NeedsRebuild_Matching(t *testing.T) {
	plugins := []string{"github.com/a/b@v1.0.0", "github.com/c/d@v2.0.0"}
	hash := BuildHash(plugins)

	bs := &Bootstrapper{currentHash: hash}
	if bs.NeedsRebuild(plugins) {
		t.Error("should not need rebuild when hashes match")
	}
}

func TestBootstrapper_NeedsRebuild_Different(t *testing.T) {
	bs := &Bootstrapper{currentHash: BuildHash([]string{"github.com/a/b@v1.0.0"})}
	if !bs.NeedsRebuild([]string{"github.com/a/b@v2.0.0"}) {
		t.Error("should need rebuild when hashes differ")
	}
}

func TestBootstrapper_NeedsRebuild_EmptyHash(t *testing.T) {
	bs := &Bootstrapper{currentHash: ""}
	if bs.NeedsRebuild([]string{"github.com/a/b@v1.0.0"}) {
		t.Error("should not need rebuild when currentHash is empty")
	}
}

func TestNewBootstrapper_ResolvesExecutable(t *testing.T) {
	bs, err := NewBootstrapper("somehash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bs.binaryPath == "" {
		t.Error("binaryPath should not be empty")
	}
	if bs.xsclawPath == "" {
		t.Error("xsclawPath should not be empty")
	}
}
