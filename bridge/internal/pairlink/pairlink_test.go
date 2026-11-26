package pairlink

import "testing"

// TestRoundTrip pins the format that travels between two independently written
// implementations: this one and the app's PairLink.kt.
func TestRoundTrip(t *testing.T) {
	original := Link{
		Address: "100.64.0.1:45999",
		Token:   "lVVUUr4n1fnQe81UpIA5zCOrBRM9JgvKOf623GsOc2U",
		Name:    "dev-laptop",
	}

	parsed, err := Parse(original.String())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed != original {
		t.Fatalf("round trip lost data: %+v vs %+v", parsed, original)
	}
}

// TestTokenSurvivesHostileChatClients covers the realistic failure: the line goes
// through a messaging app, a mail client, or a terminal that wrapped it.
func TestTokenSurvivesHostileChatClients(t *testing.T) {
	link := Link{
		Address: "100.64.0.1:45999",
		Token:   "abc-DEF_ghi.jkl~mno",
		Name:    "my box",
	}
	rendered := link.String()

	// The characters that would break an unencoded query must be escaped.
	if !containsOnlyURISafe(rendered) {
		t.Fatalf("the rendered link contains raw characters a URL parser would re-interpret: %s", rendered)
	}

	for _, variant := range []string{
		rendered,
		"  " + rendered + "\n",     // leading and trailing whitespace
		rendered + "&unexpected=1", // a client appended a tracking parameter
		"perch://PAIR?" + rendered[len("perch://pair?"):], // scheme or host case changed
	} {
		parsed, err := Parse(variant)
		if err != nil {
			t.Errorf("Parse(%q): %v", variant, err)
			continue
		}
		if parsed.Token != link.Token {
			t.Errorf("Parse(%q) token = %q, want %q", variant, parsed.Token, link.Token)
		}
		if parsed.Address != link.Address {
			t.Errorf("Parse(%q) address = %q, want %q", variant, parsed.Address, link.Address)
		}
	}
}

// TestParseRefusesAnythingElse is the security side: a link must never be able to
// point the app at an arbitrary scheme or host.
func TestParseRefusesAnythingElse(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"https://example.com/?address=a&token=b",
		"perch://elsewhere?address=a&token=b",
		"perch://pair?address=a",         // no token
		"perch://pair?token=b",           // no address
		"perch://pair?address=&token=b",  // empty address
		"perch://pair?address=a&token= ", // blank token
		"not a link at all",
	}
	for _, raw := range bad {
		if parsed, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) accepted the input as %+v", raw, parsed)
		}
	}
}

func containsOnlyURISafe(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ':', r == '/', r == '?', r == '&', r == '=', r == '%', r == '.',
			r == '-', r == '_', r == '~', r == '+', r == '#', r == '[':
		default:
			return false
		}
	}
	return true
}
