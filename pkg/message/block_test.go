package message

import (
	"encoding/json"
	"testing"
)

func TestNewTextBlock(t *testing.T) {
	b := NewTextBlock("hello")
	if b.Type != BlockText {
		t.Errorf("Type = %q, want %q", b.Type, BlockText)
	}
	if b.Text != "hello" {
		t.Errorf("Text = %q, want %q", b.Text, "hello")
	}
}

func TestNewImageBlock(t *testing.T) {
	b := NewImageBlock("https://example.com/img.png", "image/png")
	if b.Type != BlockImage {
		t.Errorf("Type = %q, want %q", b.Type, BlockImage)
	}
	if b.URL != "https://example.com/img.png" {
		t.Errorf("URL = %q, want %q", b.URL, "https://example.com/img.png")
	}
	if b.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want %q", b.MIMEType, "image/png")
	}
}

func TestNewAudioBlock(t *testing.T) {
	tests := []struct {
		name    string
		isVoice bool
	}{
		{"voice message", true},
		{"audio file", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewAudioBlock("https://example.com/audio.ogg", "audio/ogg", tt.isVoice)
			if b.Type != BlockAudio {
				t.Errorf("Type = %q, want %q", b.Type, BlockAudio)
			}
			if b.IsVoice != tt.isVoice {
				t.Errorf("IsVoice = %v, want %v", b.IsVoice, tt.isVoice)
			}
		})
	}
}

func TestNewFileBlock(t *testing.T) {
	b := NewFileBlock("https://example.com/doc.pdf", "application/pdf", "doc.pdf")
	if b.Type != BlockFile {
		t.Errorf("Type = %q, want %q", b.Type, BlockFile)
	}
	if b.FileName != "doc.pdf" {
		t.Errorf("FileName = %q, want %q", b.FileName, "doc.pdf")
	}
}

func TestNewLocationBlock(t *testing.T) {
	b := NewLocationBlock(48.8566, 2.3522)
	if b.Type != BlockLocation {
		t.Errorf("Type = %q, want %q", b.Type, BlockLocation)
	}
	if b.Lat == nil || *b.Lat != 48.8566 {
		t.Errorf("Lat = %v, want 48.8566", b.Lat)
	}
	if b.Lon == nil || *b.Lon != 2.3522 {
		t.Errorf("Lon = %v, want 2.3522", b.Lon)
	}
}

func TestNewReactionBlock(t *testing.T) {
	b := NewReactionBlock("thumbsup")
	if b.Type != BlockReaction {
		t.Errorf("Type = %q, want %q", b.Type, BlockReaction)
	}
	if b.Emoji != "thumbsup" {
		t.Errorf("Emoji = %q, want %q", b.Emoji, "thumbsup")
	}
}

func TestNewRawBlock(t *testing.T) {
	data := json.RawMessage(`{"custom":"payload"}`)
	b := NewRawBlock(data)
	if b.Type != BlockRaw {
		t.Errorf("Type = %q, want %q", b.Type, BlockRaw)
	}
	if string(b.Data) != `{"custom":"payload"}` {
		t.Errorf("Data = %s, want %s", b.Data, `{"custom":"payload"}`)
	}
}

func TestContentBlock_JSONRoundTrip(t *testing.T) {
	blocks := []ContentBlock{
		NewTextBlock("hello"),
		NewImageBlock("https://example.com/img.png", "image/png"),
		NewAudioBlock("https://example.com/voice.ogg", "audio/ogg", true),
		NewFileBlock("https://example.com/doc.pdf", "application/pdf", "doc.pdf"),
		NewLocationBlock(48.8566, 2.3522),
		NewReactionBlock("thumbsup"),
		NewRawBlock(json.RawMessage(`{"k":"v"}`)),
	}

	for _, original := range blocks {
		t.Run(string(original.Type), func(t *testing.T) {
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal error: %v", err)
			}

			var decoded ContentBlock
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if decoded.Type != original.Type {
				t.Errorf("Type = %q, want %q", decoded.Type, original.Type)
			}
			if decoded.Text != original.Text {
				t.Errorf("Text = %q, want %q", decoded.Text, original.Text)
			}
			if decoded.URL != original.URL {
				t.Errorf("URL = %q, want %q", decoded.URL, original.URL)
			}
		})
	}
}

func TestNewLocationBlock_ZeroCoordinates(t *testing.T) {
	b := NewLocationBlock(0, 0)
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded ContentBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Lat == nil {
		t.Fatal("Lat = nil, want 0")
	}
	if *decoded.Lat != 0 {
		t.Errorf("Lat = %f, want 0", *decoded.Lat)
	}
	if decoded.Lon == nil {
		t.Fatal("Lon = nil, want 0")
	}
	if *decoded.Lon != 0 {
		t.Errorf("Lon = %f, want 0", *decoded.Lon)
	}
}

func TestNonLocationBlock_NoLatLon(t *testing.T) {
	b := NewTextBlock("hello")
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := raw["lat"]; ok {
		t.Error("lat should be omitted from non-location blocks")
	}
	if _, ok := raw["lon"]; ok {
		t.Error("lon should be omitted from non-location blocks")
	}
}

func TestLocationBlock_ZeroCoordsWhenUnset(t *testing.T) {
	b := ContentBlock{Type: BlockLocation}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := raw["lat"]; !ok {
		t.Error("lat should be present for location blocks")
	}
	if _, ok := raw["lon"]; !ok {
		t.Error("lon should be present for location blocks")
	}
}

func TestNonLocationBlock_ManualCoordsAreOmitted(t *testing.T) {
	lat := 1.23
	lon := 4.56
	b := ContentBlock{
		Type: BlockText,
		Text: "hello",
		Lat:  &lat,
		Lon:  &lon,
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if _, ok := raw["lat"]; ok {
		t.Error("lat should be omitted from non-location blocks")
	}
	if _, ok := raw["lon"]; ok {
		t.Error("lon should be omitted from non-location blocks")
	}
}

func TestNewRawBlock_DefensiveCopy(t *testing.T) {
	original := json.RawMessage(`{"key":"value"}`)
	b := NewRawBlock(original)

	// Mutate the original slice.
	original[0] = 'X'

	if b.Data[0] == 'X' {
		t.Error("NewRawBlock did not make a defensive copy; mutation leaked into block")
	}
}

func TestTextContent(t *testing.T) {
	tests := []struct {
		name   string
		blocks []ContentBlock
		want   string
	}{
		{"single text", []ContentBlock{NewTextBlock("hello")}, "hello"},
		{"multiple texts", []ContentBlock{NewTextBlock("a"), NewTextBlock("b")}, "a\nb"},
		{"mixed blocks", []ContentBlock{
			NewTextBlock("hello"),
			NewImageBlock("url", "image/png"),
			NewTextBlock("world"),
		}, "hello\nworld"},
		{"no text blocks", []ContentBlock{NewImageBlock("url", "image/png")}, ""},
		{"empty slice", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := textContent(tt.blocks); got != tt.want {
				t.Errorf("textContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHasMedia(t *testing.T) {
	tests := []struct {
		name   string
		blocks []ContentBlock
		want   bool
	}{
		{"image", []ContentBlock{NewImageBlock("url", "image/png")}, true},
		{"audio", []ContentBlock{NewAudioBlock("url", "audio/ogg", false)}, true},
		{"file", []ContentBlock{NewFileBlock("url", "application/pdf", "f.pdf")}, true},
		{"location", []ContentBlock{NewLocationBlock(0, 0)}, true},
		{"text only", []ContentBlock{NewTextBlock("hello")}, false},
		{"reaction only", []ContentBlock{NewReactionBlock("ok")}, false},
		{"raw only", []ContentBlock{NewRawBlock(json.RawMessage(`{}`))}, false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasMedia(tt.blocks); got != tt.want {
				t.Errorf("hasMedia() = %v, want %v", got, tt.want)
			}
		})
	}
}
