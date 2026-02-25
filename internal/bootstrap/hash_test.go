package bootstrap

import "testing"

func TestBuildHash_Deterministic(t *testing.T) {
	plugins := []string{"github.com/a/b@v1.0.0", "github.com/c/d@v2.0.0"}
	h1 := BuildHash(plugins)
	h2 := BuildHash(plugins)
	if h1 != h2 {
		t.Errorf("non-deterministic: %q != %q", h1, h2)
	}
}

func TestBuildHash_OrderIndependent(t *testing.T) {
	h1 := BuildHash([]string{"github.com/a/b@v1.0.0", "github.com/c/d@v2.0.0"})
	h2 := BuildHash([]string{"github.com/c/d@v2.0.0", "github.com/a/b@v1.0.0"})
	if h1 != h2 {
		t.Errorf("order-dependent: %q != %q", h1, h2)
	}
}

func TestBuildHash_Different(t *testing.T) {
	h1 := BuildHash([]string{"github.com/a/b@v1.0.0"})
	h2 := BuildHash([]string{"github.com/a/b@v2.0.0"})
	if h1 == h2 {
		t.Error("different inputs produced same hash")
	}
}

func TestBuildHash_EmptyList(t *testing.T) {
	h1 := BuildHash(nil)
	h2 := BuildHash([]string{})
	if h1 != h2 {
		t.Errorf("nil vs empty: %q != %q", h1, h2)
	}
	if h1 == "" {
		t.Error("empty list should still produce a hash")
	}
}
