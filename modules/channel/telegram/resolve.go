package telegram

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/flemzord/sclaw/pkg/message"
)

const fileIDPrefix = "tg://file_id/"

// resolveMediaURLs replaces tg://file_id/ references in image blocks with
// real HTTP download URLs via the Telegram Bot API. Non-image blocks and
// blocks with non-Telegram URLs are left untouched.
func resolveMediaURLs(ctx context.Context, client *Client, msg *message.InboundMessage) error {
	for i := range msg.Blocks {
		block := &msg.Blocks[i]
		if block.Type != message.BlockImage {
			continue
		}
		if !strings.HasPrefix(block.URL, fileIDPrefix) {
			continue
		}

		fileID := strings.TrimPrefix(block.URL, fileIDPrefix)
		file, err := client.GetFile(ctx, fileID)
		if err != nil {
			return fmt.Errorf("telegram: resolve file %s: %w", fileID, err)
		}

		block.URL = client.FileURL(file.FilePath)
		if block.MIMEType == "" {
			block.MIMEType = guessImageMIME(file.FilePath)
		}
	}
	return nil
}

// guessImageMIME infers a MIME type from the file extension.
func guessImageMIME(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}
