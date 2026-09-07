package idempotency

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBodyDigestIsStableSHA256(t *testing.T) {
	if got, want := BodyDigest([]byte("crest")), "ac384d4204164a952d7af3a4226c1cd25fafa4382be588692d758673b6abc2cd"; got != want {
		t.Fatalf("digest = %q, want %q", got, want)
	}
	first := []byte("same")
	second := append([]byte(nil), first...)
	if BodyDigest(first) != BodyDigest(second) {
		t.Fatal("same body produced different digests")
	}
}

func TestNormalizeRejectsUnsafeOrIncompleteIdentity(t *testing.T) {
	cases := []Request{
		{Key: "", Actor: "a", Method: "POST", Path: "/v1/x", BodyDigest: BodyDigest(nil)},
		{Key: "k", Actor: "", Method: "POST", Path: "/v1/x", BodyDigest: BodyDigest(nil)},
		{Key: "k", Actor: "a", Method: "POST", Path: "v1/x", BodyDigest: BodyDigest(nil)},
		{Key: "k\n", Actor: "a", Method: "POST", Path: "/v1/x", BodyDigest: BodyDigest(nil)},
		{Key: "k", Actor: "a", Method: "POST", Path: "/v1/x", BodyDigest: "not-a-digest"},
	}
	for _, tc := range cases {
		if _, err := normalize(tc); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("normalize(%+v) error = %v, want ErrInvalidRequest", tc, err)
		}
	}
}

func TestNormalizeCanonicalizesMethod(t *testing.T) {
	req, err := normalize(Request{Key: "operation", Actor: "did:crest:party:a", Method: " post ", Path: "/v1/enrolments", BodyDigest: BodyDigest([]byte("{}"))})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" {
		t.Fatalf("method = %q, want POST", req.Method)
	}
}

func TestCanonicalPathIncludesSortedMutationQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/parties/p/consents?contextId=c&moment=enrolment", nil)
	r.URL.RawQuery = "moment=enrolment&contextId=c"
	if got, want := CanonicalPath(r), "/v1/parties/p/consents?contextId=c&moment=enrolment"; got != want {
		t.Fatalf("canonical path = %q, want %q", got, want)
	}
}
