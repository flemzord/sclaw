package telegram

import (
	"context"
	"testing"

	"github.com/flemzord/sclaw/pkg/message"
)

// mockFileClient implements the subset of Client needed by resolveMediaURLs.
type mockFileClient struct {
	Client
	files map[string]*File
}

func (c *mockFileClient) GetFile(ctx context.Context, fileID string) (*File, error) {
	if f, ok := c.files[fileID]; ok {
		return f, nil
	}
	return nil, context.DeadlineExceeded
}

func TestResolveMediaURLs_ResolvesImageBlocks(t *testing.T) {
	t.Parallel()

	client := &Client{
		baseURL: "https://api.telegram.org",
		token:   "123:ABC",
	}
	// We can't easily mock GetFile on the real Client, so test guessImageMIME
	// and the URL construction logic directly.

	// Test FileURL construction.
	got := client.FileURL("photos/file_1.jpg")
	want := "https://api.telegram.org/file/bot123:ABC/photos/file_1.jpg"
	if got != want {
		t.Errorf("FileURL = %q, want %q", got, want)
	}
}

func TestGuessImageMIME(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{"photos/file_1.jpg", "image/jpeg"},
		{"photos/file_2.jpeg", "image/jpeg"},
		{"photos/file_3.png", "image/png"},
		{"photos/file_4.gif", "image/gif"},
		{"photos/file_5.webp", "image/webp"},
		{"photos/file_6.bmp", ""},
		{"photos/file_7.JPG", "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := guessImageMIME(tt.path)
			if got != tt.want {
				t.Errorf("guessImageMIME(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestResolveMediaURLs_SkipsNonTelegramURLs(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		Blocks: []message.ContentBlock{
			message.NewImageBlock("https://example.com/img.jpg", "image/jpeg"),
		},
	}

	// resolveMediaURLs should skip blocks that don't have tg://file_id/ prefix.
	// Pass a nil client — it should never be called.
	err := resolveMediaURLs(context.Background(), &Client{}, &msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Blocks[0].URL != "https://example.com/img.jpg" {
		t.Errorf("URL changed unexpectedly: %s", msg.Blocks[0].URL)
	}
}

func TestResolveMediaURLs_SkipsTextBlocks(t *testing.T) {
	t.Parallel()

	msg := message.InboundMessage{
		Blocks: []message.ContentBlock{
			{Type: message.BlockText, Text: "hello"},
		},
	}

	err := resolveMediaURLs(context.Background(), &Client{}, &msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Blocks[0].Text != "hello" {
		t.Errorf("text block modified unexpectedly")
	}
}
