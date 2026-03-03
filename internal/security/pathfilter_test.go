package security

import (
	"errors"
	"testing"
)

func TestPathFilterCheckRead(t *testing.T) {
	t.Parallel()

	filter := NewPathFilter(PathFilterConfig{
		AllowedDirs: []AllowedDir{
			{Path: "/home/user/project", Mode: PathAccessRO},
			{Path: "/home/user/documents", Mode: PathAccessRW},
		},
	})

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "file in RO dir", path: "/home/user/project/main.go", wantErr: false},
		{name: "file in RW dir", path: "/home/user/documents/notes.txt", wantErr: false},
		{name: "dir root RO", path: "/home/user/project", wantErr: false},
		{name: "dir root RW", path: "/home/user/documents", wantErr: false},
		{name: "nested in RO", path: "/home/user/project/src/lib/util.go", wantErr: false},
		{name: "outside all dirs", path: "/tmp/secret.txt", wantErr: true},
		{name: "parent of allowed dir", path: "/home/user", wantErr: true},
		{name: "prefix collision", path: "/home/user/project-extra/file.go", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := filter.CheckRead(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckRead(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrPathBlocked) {
				t.Errorf("CheckRead(%q) error should wrap ErrPathBlocked, got %v", tt.path, err)
			}
		})
	}
}

func TestPathFilterCheckWrite(t *testing.T) {
	t.Parallel()

	filter := NewPathFilter(PathFilterConfig{
		AllowedDirs: []AllowedDir{
			{Path: "/home/user/project", Mode: PathAccessRO},
			{Path: "/home/user/documents", Mode: PathAccessRW},
		},
	})

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "write in RW dir", path: "/home/user/documents/output.txt", wantErr: false},
		{name: "write in RO dir rejected", path: "/home/user/project/main.go", wantErr: true},
		{name: "write outside all dirs", path: "/tmp/file.txt", wantErr: true},
		{name: "RW dir root", path: "/home/user/documents", wantErr: false},
		{name: "RO dir root rejected", path: "/home/user/project", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := filter.CheckWrite(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckWrite(%q) error = %v, wantErr = %v", tt.path, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrPathBlocked) {
				t.Errorf("CheckWrite(%q) error should wrap ErrPathBlocked, got %v", tt.path, err)
			}
		})
	}
}

func TestPathFilterModeNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     PathAccessMode
		wantMode PathAccessMode
	}{
		{name: "empty defaults to ro", mode: "", wantMode: PathAccessRO},
		{name: "invalid defaults to ro", mode: "invalid", wantMode: PathAccessRO},
		{name: "ro preserved", mode: PathAccessRO, wantMode: PathAccessRO},
		{name: "rw preserved", mode: PathAccessRW, wantMode: PathAccessRW},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			filter := NewPathFilter(PathFilterConfig{
				AllowedDirs: []AllowedDir{{Path: "/test", Mode: tt.mode}},
			})
			dirs := filter.Dirs()
			if len(dirs) != 1 {
				t.Fatalf("expected 1 dir, got %d", len(dirs))
			}
			if dirs[0].Mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", dirs[0].Mode, tt.wantMode)
			}
		})
	}
}

func TestPathFilterRelativePathsIgnored(t *testing.T) {
	t.Parallel()

	filter := NewPathFilter(PathFilterConfig{
		AllowedDirs: []AllowedDir{
			{Path: "relative/path", Mode: PathAccessRO},
			{Path: "./another", Mode: PathAccessRW},
			{Path: "/absolute/path", Mode: PathAccessRO},
		},
	})

	dirs := filter.Dirs()
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir (relative should be dropped), got %d", len(dirs))
	}
	if dirs[0].Path != "/absolute/path" {
		t.Errorf("expected /absolute/path, got %s", dirs[0].Path)
	}
}

func TestPathFilterDirsReturnsCopy(t *testing.T) {
	t.Parallel()

	filter := NewPathFilter(PathFilterConfig{
		AllowedDirs: []AllowedDir{
			{Path: "/test", Mode: PathAccessRO},
		},
	})

	dirs := filter.Dirs()
	dirs[0].Path = "/hacked"

	original := filter.Dirs()
	if original[0].Path != "/test" {
		t.Error("Dirs() should return a copy, but modifying it changed the original")
	}
}

func TestPathFilterEmpty(t *testing.T) {
	t.Parallel()

	filter := NewPathFilter(PathFilterConfig{})

	if err := filter.CheckRead("/any/path"); err == nil {
		t.Error("CheckRead should fail with empty filter")
	}
	if err := filter.CheckWrite("/any/path"); err == nil {
		t.Error("CheckWrite should fail with empty filter")
	}
	if dirs := filter.Dirs(); len(dirs) != 0 {
		t.Errorf("expected 0 dirs, got %d", len(dirs))
	}
}

func TestPathFilterPathCleaning(t *testing.T) {
	t.Parallel()

	filter := NewPathFilter(PathFilterConfig{
		AllowedDirs: []AllowedDir{
			{Path: "/home/user/project/", Mode: PathAccessRO},
		},
	})

	dirs := filter.Dirs()
	if len(dirs) != 1 {
		t.Fatal("expected 1 dir")
	}
	// filepath.Clean removes trailing slash.
	if dirs[0].Path != "/home/user/project" {
		t.Errorf("expected cleaned path /home/user/project, got %s", dirs[0].Path)
	}

	// CheckRead should still work with the cleaned path.
	if err := filter.CheckRead("/home/user/project/file.go"); err != nil {
		t.Errorf("CheckRead should pass after path cleaning: %v", err)
	}
}
