package parties

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidVoiceRecordingRequiresMIMEAndMagic(t *testing.T) {
	tests := []struct {
		name string
		mime string
		body string
		want bool
	}{
		{name: "ogg", mime: "audio/ogg; codecs=opus", body: "OggS\x00voice", want: true},
		{name: "wav", mime: "audio/wav", body: "RIFF1234WAVEfmt ", want: true},
		{name: "text masquerading as ogg", mime: "audio/ogg", body: "worker consent", want: false},
		{name: "wrong MIME", mime: "text/plain", body: "OggS\x00voice", want: false},
		{name: "short WAV", mime: "audio/wav", body: "RIFF", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validVoiceRecording(tt.mime, []byte(tt.body)); got != tt.want {
				t.Fatalf("validVoiceRecording(%q, %q) = %v, want %v", tt.mime, tt.body, got, tt.want)
			}
		})
	}
}

func TestReadIdempotentJSONFingerprintsExactWireBytes(t *testing.T) {
	type request struct {
		ContextID string `json:"contextId"`
	}
	first := httptest.NewRequest("POST", "/v1/enrolments", strings.NewReader(`{"contextId":"ctx"}`))
	var got request
	raw, ok := readIdempotentJSON(httptest.NewRecorder(), first, &got)
	if !ok {
		t.Fatal("valid JSON was rejected")
	}
	if string(raw) != `{"contextId":"ctx"}` || got.ContextID != "ctx" {
		t.Fatalf("raw=%q decoded=%+v", raw, got)
	}

	second := httptest.NewRequest("POST", "/v1/enrolments", strings.NewReader(`{"contextId": "ctx"}`))
	var equivalent request
	raw2, ok := readIdempotentJSON(httptest.NewRecorder(), second, &equivalent)
	if !ok || string(raw) == string(raw2) {
		t.Fatalf("equivalent JSON must retain distinct wire fingerprints: %q %q", raw, raw2)
	}
}

func TestReadIdempotentJSONRejectsConcatenatedValues(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/enrolments", strings.NewReader(`{} {}`))
	var body map[string]any
	if _, ok := readIdempotentJSON(rec, r, &body); ok {
		t.Fatal("concatenated JSON values were accepted")
	}
	if rec.Code != 400 {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
}

func TestRequireIdempotencyKeyFailsClosed(t *testing.T) {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/enrolments", nil)
	if _, ok := requireIdempotencyKey(rec, r); ok {
		t.Fatal("missing idempotency key was accepted")
	}
	if rec.Code != 400 {
		t.Fatalf("status=%d, want 400", rec.Code)
	}
	r.Header.Set("Idempotency-Key", "   ")
	rec = httptest.NewRecorder()
	if _, ok := requireIdempotencyKey(rec, r); ok || rec.Code != 400 {
		t.Fatalf("blank idempotency key accepted: ok=%v status=%d", ok, rec.Code)
	}
}
