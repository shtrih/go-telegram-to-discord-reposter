package handler

import (
	"errors"
	"net/http"
	"testing"

	"github.com/OvyFlash/telegram-bot-api"
	"github.com/bwmarrin/discordgo"
)

func TestMediaFileSize(t *testing.T) {
	tests := []struct {
		name string
		post *tgbotapi.Message
		want int64
	}{
		{"no media", &tgbotapi.Message{}, 0},
		{"document", &tgbotapi.Message{Document: &tgbotapi.Document{FileSize: 25 << 20}}, 25 << 20},
		{"video", &tgbotapi.Message{Video: &tgbotapi.Video{FileSize: 80 << 20}}, 80 << 20},
		{"voice", &tgbotapi.Message{Voice: &tgbotapi.Voice{FileSize: 3 << 20}}, 3 << 20},
		{"photo takes largest size", &tgbotapi.Message{Photo: []tgbotapi.PhotoSize{{FileSize: 1000}, {FileSize: 3000}}}, 3000},
		{"sticker thumbnail", &tgbotapi.Message{Sticker: &tgbotapi.Sticker{Thumbnail: &tgbotapi.PhotoSize{FileSize: 500}}}, 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mediaFileSize(tc.post); got != tc.want {
				t.Fatalf("mediaFileSize() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestIsTooLargeErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"telegram file too big", errors.New("Bad Request: file is too big"), true},
		{"other telegram error", errors.New("Bad Request: wrong file id"), false},
		{"discord 413", &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusRequestEntityTooLarge}}, true},
		{"discord 400", &discordgo.RESTError{Response: &http.Response{StatusCode: http.StatusBadRequest}}, false},
		{"generic", errors.New("connection reset by peer"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTooLargeErr(tc.err); got != tc.want {
				t.Fatalf("isTooLargeErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestResendButtonLabel(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"one file", 1, "Переслать без файла"},
		{"two files", 2, "Переслать без этих файлов"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resendButtonLabel(tc.n); got != tc.want {
				t.Fatalf("resendButtonLabel(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

func TestOversizedNoticeText(t *testing.T) {
	item := func(fileName string, sizeBytes int64, caption string) *oversizedItem {
		return &oversizedItem{
			post:      &tgbotapi.Message{Caption: caption},
			fileName:  fileName,
			sizeBytes: sizeBytes,
		}
	}
	tests := []struct {
		name  string
		items []*oversizedItem
		want  string
	}{
		{
			"one file with text asks",
			[]*oversizedItem{item("a.zip", 25<<20, "caption")},
			"⚠️ Файл не отправлен — превышен лимит 20 MiB:\n• a.zip — 25.0 MiB\n\nОтправить пост без файла?",
		},
		{
			"several files with text ask in plural",
			[]*oversizedItem{item("a.zip", 25<<20, ""), item("b.mkv", 80<<20, "caption")},
			"⚠️ Файлы не отправлены — превышен лимит 20 MiB:\n• a.zip — 25.0 MiB\n• b.mkv — 80.0 MiB\n\nОтправить посты без этих файлов?",
		},
		{
			"one file without text leaves nothing to ask",
			[]*oversizedItem{item("a.zip", 25<<20, "")},
			"⚠️ Файл не отправлен — превышен лимит 20 MiB:\n• a.zip — 25.0 MiB\n",
		},
		{
			"several files without text leave nothing to ask",
			[]*oversizedItem{item("a.zip", 25<<20, ""), item("b.mkv", 80<<20, "")},
			"⚠️ Файлы не отправлены — превышен лимит 20 MiB:\n• a.zip — 25.0 MiB\n• b.mkv — 80.0 MiB\n",
		},
		{
			"unknown size has no size suffix",
			[]*oversizedItem{item("a.zip", 0, "caption")},
			"⚠️ Файл не отправлен — превышен лимит 20 MiB:\n• a.zip\n\nОтправить пост без файла?",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := oversizedNoticeText(20, tc.items); got != tc.want {
				t.Fatalf("oversizedNoticeText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseCallback(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		key   string
		skip  bool
		valid bool
	}{
		{"resend", sendPrefix + "-1001,5", "-1001,5", false, true},
		{"skip", skipPrefix + "-1001,5", "-1001,5", true, true},
		{"other button", "other:-1001,5", "", false, false},
		{"empty", "", "", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, skip, ok := parseCallback(tc.data)
			if key != tc.key || skip != tc.skip || ok != tc.valid {
				t.Fatalf("parseCallback(%q) = (%q, %v, %v), want (%q, %v, %v)", tc.data, key, skip, ok, tc.key, tc.skip, tc.valid)
			}
		})
	}
}

func TestRetryStore(t *testing.T) {
	r := NewRetryStore()

	// Empty store flushes nothing.
	if items := r.takeIfChanged(""); len(items) != 0 {
		t.Fatalf("takeIfChanged on empty store returned %d items", len(items))
	}

	r.AddOversized(nil, nil, &oversizedItem{fileName: "a"}, "g1")
	r.AddOversized(nil, nil, &oversizedItem{fileName: "b"}, "g1")

	// Same media group: no flush.
	if items := r.takeIfChanged("g1"); len(items) != 0 {
		t.Fatalf("same group flushed %d items", len(items))
	}

	// Another group flushes everything.
	if items := r.takeIfChanged(""); len(items) != 2 {
		t.Fatalf("group change flushed %d items, want 2", len(items))
	}
	if items := r.takeBuffered(); len(items) != 0 {
		t.Fatalf("buffer not empty after flush: %d items", len(items))
	}

	// Standalone post is flushed by an unconditional take.
	r.AddOversized(nil, nil, &oversizedItem{fileName: "c"}, "")
	if items := r.takeBuffered(); len(items) != 1 {
		t.Fatalf("takeBuffered returned %d items, want 1", len(items))
	}

	// Pending notices pop exactly once.
	r.addPending("k", &pendingRetry{noticeMsgID: 1})
	if p, ok := r.popPending("k"); !ok || p.noticeMsgID != 1 {
		t.Fatalf("popPending ok=%v, noticeMsgID=%d", ok, p.noticeMsgID)
	}
	if _, ok := r.popPending("k"); ok {
		t.Fatal("popPending returned the same notice twice")
	}
}
