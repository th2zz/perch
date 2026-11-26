package pairlink

import (
	"os"
	"testing"

	"github.com/th2zz/perch/bridge/internal/testfixture"
)

// The pairing link is the product's first step and the only string that crosses
// from the bridge to the app. It has two implementations — this one and
// `PairLink.kt` — and nothing until now made them agree beyond two people typing
// similar literals into two test files. That is exactly the kind of "both sides
// are green and the product is broken" seam this repository keeps finding: the Go
// encoder could start escaping `:` differently, every Go test would still pass,
// the Kotlin tests would still pass, and the user's first tap would do nothing.
//
// So the encoded bytes live in one file that both languages read:
// `contracts/pairlink-cases.json`. Go owns the encoding (it
// is the producer), so this test is the one that can regenerate it; the Kotlin
// test only reads it. If either side drifts, one of the two goes red.
//
// Regenerate after an intentional change to the format:
//
//	PERCH_UPDATE_PAIRLINK_FIXTURE=1 go test ./internal/pairlink/ -run Fixture

const fixtureRelPath = "contracts/pairlink-cases.json"

// fixtureCase is one link, with the exact bytes the encoder produces written back
// into Link. The Why field is the reason the case exists, kept in the file so a
// reader can see what each line is pinning.
type fixtureCase struct {
	Why     string `json:"why"`
	Address string `json:"address"`
	Token   string `json:"token"`
	Name    string `json:"name"`
	Link    string `json:"link"`
}

// fixtureCases are the inputs. The cases are chosen for the characters that a URI
// encoder and a chat client are each likely to mangle: the `:` and `/` of the
// address, the `+`, `/` and `=` that turn up in a token, and spaces, `&`, `=` and
// `%` in the computer's name.
var fixtureCases = []fixtureCase{
	{
		Why:     "the ordinary case: a Tailscale address, a base64url token, a hostname",
		Address: "100.64.0.1:45999",
		Token:   "hL0a_-Qx9Kd2Wz",
		Name:    "dev-laptop",
	},
	{
		Why:     "the name has a space: Go writes it as '+', and the app must read it back as a space",
		Address: "100.64.0.1:45999",
		Token:   "hL0a_-Qx9Kd2Wz",
		Name:    "Home Mac",
	},
	{
		Why:     "a token with standard-base64 characters: '+' and '/' and '=' must survive, which is the case a naive decoder corrupts",
		Address: "100.64.0.1:45999",
		Token:   "a+b/c=",
		Name:    "mac",
	},
	{
		Why:     "the name has every query-string metacharacter at once",
		Address: "100.64.0.1:45999",
		Token:   "hL0a_-Qx9Kd2Wz",
		Name:    "R&D = 50% +1",
	},
	{
		Why:     "the address is a URL rather than host:port, which the app accepts either way",
		Address: "http://my-box.local:45999",
		Token:   "hL0a_-Qx9Kd2Wz",
		Name:    "box",
	},
	{
		Why:     "no name at all: the parameter is omitted, and the app must not treat that as a broken link",
		Address: "100.64.0.1:45999",
		Token:   "hL0a_-Qx9Kd2Wz",
		Name:    "",
	},
}

func TestFixtureIsTheBytesTheEncoderProduces(t *testing.T) {
	// The update path is how the file was made and how it is remade after an
	// intentional format change. It is a separate env var rather than a flag so
	// that a plain `go test ./...` can never rewrite a contract by accident.
	if os.Getenv("PERCH_UPDATE_PAIRLINK_FIXTURE") == "1" {
		out := make([]fixtureCase, len(fixtureCases))
		for i, c := range fixtureCases {
			out[i] = c
			out[i].Link = Link{Address: c.Address, Token: c.Token, Name: c.Name}.String()
		}
		testfixture.WriteJSON(t, fixtureRelPath, out)
		return
	}

	var stored []fixtureCase
	testfixture.ReadJSON(t, fixtureRelPath, &stored)

	if len(stored) != len(fixtureCases) {
		t.Fatalf("the fixture has %d cases and this test has %d; regenerate with PERCH_UPDATE_PAIRLINK_FIXTURE=1",
			len(stored), len(fixtureCases))
	}

	for i, want := range stored {
		got := fixtureCases[i]
		if got.Address != want.Address || got.Token != want.Token || got.Name != want.Name {
			t.Errorf("case %d inputs drifted from the fixture; regenerate with PERCH_UPDATE_PAIRLINK_FIXTURE=1", i)
			continue
		}

		// What the bridge actually prints must equal the checked-in bytes, character
		// for character. This is the half the app cannot check: it never sees the
		// encoder.
		if rendered := (Link{Address: want.Address, Token: want.Token, Name: want.Name}).String(); rendered != want.Link {
			t.Errorf("case %d (%s):\n  encoder wrote  %s\n  fixture has   %s\nthe app reads the fixture, so a drift here breaks the first tap",
				i, want.Why, rendered, want.Link)
		}

		// And the bridge can read back what it wrote, which the app also relies on
		// for the pasted-link fallback.
		parsed, err := Parse(want.Link)
		if err != nil {
			t.Errorf("case %d (%s): the encoder produced a link its own parser rejects: %v", i, want.Why, err)
			continue
		}
		if parsed.Address != want.Address || parsed.Token != want.Token || parsed.Name != want.Name {
			t.Errorf("case %d (%s): round trip lost data: %+v", i, want.Why, parsed)
		}
	}
}
