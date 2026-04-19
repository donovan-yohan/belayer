package daemon

import (
	"strings"
	"testing"
)

func TestScrub_ReplacesAuthKeys(t *testing.T) {
	in := `{"api_key":"sk-abc123def456ghi789","authorization":"Bearer xyz","other":"value"}`
	got := Scrub(in)
	if strings.Contains(got, "sk-abc123def456ghi789") {
		t.Fatalf("api key leaked: %s", got)
	}
	if strings.Contains(got, "Bearer xyz") {
		t.Fatalf("bearer leaked: %s", got)
	}
	if !strings.Contains(got, `"other":"value"`) {
		t.Fatalf("non-secret field removed: %s", got)
	}
}

func TestScrub_FreeTextOpenAIToken(t *testing.T) {
	in := "the request used sk-abcdefghijklmnopqrstuvwxyz and succeeded"
	got := Scrub(in)
	if strings.Contains(got, "sk-abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("free-text sk-token leaked: %s", got)
	}
	if !strings.Contains(got, "the request used") {
		t.Fatalf("context lost: %s", got)
	}
}

func TestScrub_FreeTextBearerHeader(t *testing.T) {
	in := "Authorization: Bearer abc.def-ghi"
	got := Scrub(in)
	if strings.Contains(got, "abc.def-ghi") {
		t.Fatalf("bearer leaked: %s", got)
	}
	if !strings.Contains(got, "Bearer <redacted>") {
		t.Fatalf("bearer not replaced: %s", got)
	}
}

func TestScrub_CaseInsensitiveKeyName(t *testing.T) {
	in := `{"API_Key":"secret","Authorization":"zzz"}`
	got := Scrub(in)
	if strings.Contains(got, `"secret"`) {
		t.Fatalf("uppercased api key leaked: %s", got)
	}
}

func TestScrub_PreservesNonSecretJSON(t *testing.T) {
	in := `{"name":"alice","email":"a@b.com","status":"ok"}`
	got := Scrub(in)
	if got != in {
		t.Fatalf("non-secret JSON mutated: want %s, got %s", in, got)
	}
}
