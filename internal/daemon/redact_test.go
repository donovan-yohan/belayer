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

func TestScrub_RedactsGitHubClassicToken(t *testing.T) {
	in := "token ghp_0123456789abcdefghijABCDEFGHIJklmnop"
	got := Scrub(in)
	if strings.Contains(got, "ghp_0123456789abcdefghijABCDEFGHIJklmnop") {
		t.Fatalf("GitHub classic token leaked: %s", got)
	}
}

func TestScrub_RedactsGitHubFineGrainedPAT(t *testing.T) {
	// Real fine-grained PATs are 82 chars after the github_pat_ prefix.
	in := "github_pat_11ABCDEFGH0abcdefghijKLMNOPQRSTUVWXYZabcdefghij1234567890ABCDEFGHIJKLmnopqrstuvwxy"
	got := Scrub(in)
	if strings.Contains(got, "github_pat_") {
		t.Fatalf("GitHub fine-grained PAT leaked: %s", got)
	}
}

func TestScrub_RedactsAWSAccessKey(t *testing.T) {
	in := "key=AKIAIOSFODNN7EXAMPLE"
	got := Scrub(in)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("AWS access key leaked: %s", got)
	}
}

func TestScrub_RedactsAnthropicKey(t *testing.T) {
	in := "the request used sk-ant-api03-abcdefghij1234567890abcdefghij and succeeded"
	got := Scrub(in)
	if strings.Contains(got, "sk-ant-api03-abcdefghij1234567890abcdefghij") {
		t.Fatalf("Anthropic key leaked: %s", got)
	}
}

func TestScrub_RedactsJWT(t *testing.T) {
	in := "token: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456"
	got := Scrub(in)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Fatalf("JWT leaked: %s", got)
	}
}

func TestScrub_RedactsPEMPrivateKey(t *testing.T) {
	in := "-----BEGIN RSA PRIVATE KEY-----\nMIIEogIBAAK...\n-----END RSA PRIVATE KEY-----"
	got := Scrub(in)
	if strings.Contains(got, "MIIEogIBAAK") {
		t.Fatalf("PEM private key leaked: %s", got)
	}
}

func TestScrub_RedactsLowercaseBearer(t *testing.T) {
	in := "authorization: bearer abc123"
	got := Scrub(in)
	if strings.Contains(got, "abc123") {
		t.Fatalf("lowercase bearer leaked: %s", got)
	}
}

func TestScrub_RedactsBearerWithTabs(t *testing.T) {
	in := "Authorization: Bearer\tabc123"
	got := Scrub(in)
	if strings.Contains(got, "abc123") {
		t.Fatalf("bearer with tab leaked: %s", got)
	}
}

func TestScrub_RedactsSlackToken(t *testing.T) {
	in := "slack_token=xoxb-123456789012-123456789012-abcdefghijklmnopqrstuvwx"
	got := Scrub(in)
	if strings.Contains(got, "xoxb-") {
		t.Fatalf("Slack token leaked: %s", got)
	}
}

func TestScrub_RedactsGoogleAPIKey(t *testing.T) {
	in := "key=AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe"
	got := Scrub(in)
	if strings.Contains(got, "AIzaSyDaGmWKa4JsXZ-HjGw7ISLn_3namBGewQe") {
		t.Fatalf("Google API key leaked: %s", got)
	}
}

func TestScrub_PreservesUUID(t *testing.T) {
	// keyRegex will redact the "token" key-value pair — that's expected conservative behaviour.
	in := `{"token": "550e8400-e29b-41d4-a716-446655440000"}`
	got := Scrub(in)
	if strings.Contains(got, "550e8400-e29b-41d4-a716-446655440000") {
		// The whole key-value pair should have been redacted.
		t.Fatalf("token field not redacted: %s", got)
	}
}

func TestScrub_PreservesNonSecretCode(t *testing.T) {
	in := `if err := db.Ping(); err != nil {`
	got := Scrub(in)
	if got != in {
		t.Fatalf("non-secret code mutated: want %s, got %s", in, got)
	}
}
