package memory_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/flemzord/sclaw/internal/memory"
	"github.com/flemzord/sclaw/internal/provider"
)

// Compile-time interface guard.
var _ memory.HistoryStore = (*memory.InMemoryHistoryStore)(nil)

func testMsg(content string) provider.LLMMessage {
	return provider.LLMMessage{Role: provider.MessageRoleUser, Content: content}
}

func TestInMemoryHistoryStore_AppendAndGetAll(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryHistoryStore()

	msgs := []provider.LLMMessage{
		{Role: provider.MessageRoleUser, Content: "hello"},
		{Role: provider.MessageRoleAssistant, Content: "hi there"},
		{Role: provider.MessageRoleUser, Content: "how are you?"},
	}

	for _, m := range msgs {
		if err := store.Append("s1", m); err != nil {
			t.Fatalf("Append: unexpected error: %v", err)
		}
	}

	all, err := store.GetAll("s1")
	if err != nil {
		t.Fatalf("GetAll: unexpected error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("GetAll: got %d messages, want 3", len(all))
	}
	for i, m := range all {
		if m.Content != msgs[i].Content {
			t.Errorf("GetAll[%d].Content = %q, want %q", i, m.Content, msgs[i].Content)
		}
		if m.Role != msgs[i].Role {
			t.Errorf("GetAll[%d].Role = %q, want %q", i, m.Role, msgs[i].Role)
		}
	}
}

func TestInMemoryHistoryStore_GetRecent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		n         int
		wantLen   int
		wantFirst string // Content of first returned message
	}{
		{name: "n < available", n: 3, wantLen: 3, wantFirst: "msg-2"},
		{name: "n > available", n: 10, wantLen: 5, wantFirst: "msg-0"},
		{name: "n = available", n: 5, wantLen: 5, wantFirst: "msg-0"},
		{name: "n = 0", n: 0, wantLen: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := memory.NewInMemoryHistoryStore()
			for i := 0; i < 5; i++ {
				if err := store.Append("s1", testMsg(fmt.Sprintf("msg-%d", i))); err != nil {
					t.Fatalf("Append(%d): unexpected error: %v", i, err)
				}
			}

			recent, err := store.GetRecent("s1", tt.n)
			if err != nil {
				t.Fatalf("GetRecent(%d): unexpected error: %v", tt.n, err)
			}
			if len(recent) != tt.wantLen {
				t.Fatalf("GetRecent(%d): got %d messages, want %d", tt.n, len(recent), tt.wantLen)
			}
			if tt.wantLen > 0 && recent[0].Content != tt.wantFirst {
				t.Errorf("GetRecent(%d)[0].Content = %q, want %q", tt.n, recent[0].Content, tt.wantFirst)
			}
		})
	}
}

func TestInMemoryHistoryStore_GetRecent_NonexistentSession(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryHistoryStore()

	msgs, err := store.GetRecent("nonexistent", 5)
	if err != nil {
		t.Fatalf("GetRecent: unexpected error: %v", err)
	}
	if msgs != nil {
		t.Fatalf("GetRecent: got %v, want nil", msgs)
	}
}

func TestInMemoryHistoryStore_GetAll_ReturnsCopy(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryHistoryStore()

	if err := store.Append("s1", testMsg("original")); err != nil {
		t.Fatalf("Append: unexpected error: %v", err)
	}

	// Get a copy and mutate it.
	all, err := store.GetAll("s1")
	if err != nil {
		t.Fatalf("GetAll: unexpected error: %v", err)
	}
	all[0].Content = "mutated"

	// The store should still have the original.
	all2, err := store.GetAll("s1")
	if err != nil {
		t.Fatalf("GetAll (second): unexpected error: %v", err)
	}
	if all2[0].Content != "original" {
		t.Fatalf("GetAll after mutation: got %q, want %q", all2[0].Content, "original")
	}
}

func TestInMemoryHistoryStore_SetSummaryAndGetSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		set  string
		want string
	}{
		{name: "set and get", set: "summary text", want: "summary text"},
		{name: "overwrite", set: "overwritten", want: "overwritten"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := memory.NewInMemoryHistoryStore()

			// Set initial summary.
			if err := store.SetSummary("s1", "initial"); err != nil {
				t.Fatalf("SetSummary(initial): unexpected error: %v", err)
			}

			// Set the test summary (may overwrite).
			if err := store.SetSummary("s1", tt.set); err != nil {
				t.Fatalf("SetSummary(%q): unexpected error: %v", tt.set, err)
			}

			got, err := store.GetSummary("s1")
			if err != nil {
				t.Fatalf("GetSummary: unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("GetSummary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInMemoryHistoryStore_GetSummary_NonexistentSession(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryHistoryStore()

	summary, err := store.GetSummary("nonexistent")
	if err != nil {
		t.Fatalf("GetSummary: unexpected error: %v", err)
	}
	if summary != "" {
		t.Fatalf("GetSummary = %q, want empty string", summary)
	}
}

func TestInMemoryHistoryStore_Purge(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryHistoryStore()

	for i := 0; i < 3; i++ {
		if err := store.Append("s1", testMsg(fmt.Sprintf("msg-%d", i))); err != nil {
			t.Fatalf("Append(%d): unexpected error: %v", i, err)
		}
	}
	if err := store.SetSummary("s1", "some summary"); err != nil {
		t.Fatalf("SetSummary: unexpected error: %v", err)
	}

	if err := store.Purge("s1"); err != nil {
		t.Fatalf("Purge: unexpected error: %v", err)
	}

	// Messages should be gone.
	msgs, err := store.GetRecent("s1", 10)
	if err != nil {
		t.Fatalf("GetRecent after Purge: unexpected error: %v", err)
	}
	if msgs != nil {
		t.Fatalf("GetRecent after Purge: got %v, want nil", msgs)
	}

	// GetAll should also return nil.
	all, err := store.GetAll("s1")
	if err != nil {
		t.Fatalf("GetAll after Purge: unexpected error: %v", err)
	}
	if all != nil {
		t.Fatalf("GetAll after Purge: got %v, want nil", all)
	}

	// Summary should be gone.
	summary, err := store.GetSummary("s1")
	if err != nil {
		t.Fatalf("GetSummary after Purge: unexpected error: %v", err)
	}
	if summary != "" {
		t.Fatalf("GetSummary after Purge = %q, want empty", summary)
	}

	// Len should be 0.
	length, err := store.Len("s1")
	if err != nil {
		t.Fatalf("Len after Purge: unexpected error: %v", err)
	}
	if length != 0 {
		t.Fatalf("Len after Purge = %d, want 0", length)
	}
}

func TestInMemoryHistoryStore_Len(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		appends   int
		want      int
	}{
		{name: "nonexistent session", sessionID: "nonexistent", appends: 0, want: 0},
		{name: "after 3 appends", sessionID: "s1", appends: 3, want: 3},
		{name: "after 1 append", sessionID: "s2", appends: 1, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := memory.NewInMemoryHistoryStore()
			for i := 0; i < tt.appends; i++ {
				if err := store.Append(tt.sessionID, testMsg(fmt.Sprintf("msg-%d", i))); err != nil {
					t.Fatalf("Append(%d): unexpected error: %v", i, err)
				}
			}

			length, err := store.Len(tt.sessionID)
			if err != nil {
				t.Fatalf("Len: unexpected error: %v", err)
			}
			if length != tt.want {
				t.Fatalf("Len = %d, want %d", length, tt.want)
			}
		})
	}
}

func TestInMemoryHistoryStore_Concurrent(t *testing.T) {
	t.Parallel()

	store := memory.NewInMemoryHistoryStore()

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(goroutine int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				if err := store.Append("s1", testMsg(fmt.Sprintf("g%d-msg%d", goroutine, i))); err != nil {
					t.Errorf("Append from goroutine %d, msg %d: unexpected error: %v", goroutine, i, err)
				}
				// Interleave reads to stress the RWMutex.
				if _, err := store.GetRecent("s1", 5); err != nil {
					t.Errorf("GetRecent from goroutine %d: unexpected error: %v", goroutine, err)
				}
			}
		}(g)
	}
	wg.Wait()

	length, err := store.Len("s1")
	if err != nil {
		t.Fatalf("Len: unexpected error: %v", err)
	}
	if length != 1000 {
		t.Fatalf("Len = %d, want 1000", length)
	}
}
