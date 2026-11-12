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
	r.addPending("k", &pendingRetry{errMsgID: "m1"})
	if p, ok := r.popPending("k"); !ok || p.errMsgID != "m1" {
		t.Fatalf("popPending ok=%v, errMsgID=%q", ok, p.errMsgID)
	}
	if _, ok := r.popPending("k"); ok {
		t.Fatal("popPending returned the same notice twice")
	}
}
