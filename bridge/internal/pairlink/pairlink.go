// Package pairlink defines the one string that has to get from the computer to
// the phone.
//
// The spec's gate is that pairing involves no typing and no vocabulary
// (SC-004, SC-005): if the first step needs the user to understand an address,
// the product has already lost. Two pieces of information must cross the gap —
// where the computer is, and a secret that proves the phone may talk to it — and
// the cheapest reliable way to move them is one line the user can send to
// themselves through whatever app they already use, and then tap.
//
// The same string is what a QR code would carry, so this format is the contract
// either way: adding a camera later changes the reader, not the payload.
package pairlink

import (
	"fmt"
	"net/url"
	"strings"
)

// Scheme is the URI scheme the app registers.
const Scheme = "perch"

// Host is the only host we accept, so a link cannot aim the app anywhere else.
const Host = "pair"

// Link is the content of a pairing link.
type Link struct {
	// Address is "host:port" (or a full URL); the app accepts either.
	Address string
	// Token is the shared secret the bridge generated.
	Token string
	// Name is the computer's label, shown to the user so they can tell two
	// machines apart.
	Name string
}

// String renders the link.
//
// Details that matter:
//   - The token is in the query, not the fragment. A fragment is not sent to
//     servers, which sounds safer, but it is also dropped by some chat clients'
//     link handling, and a pairing link that arrives truncated is worse than one
//     that is merely well-formed. The link only ever goes to the user's own app.
//   - Everything is percent-encoded, because a token is base64url and a chat app
//     must not be able to mangle it.
func (l Link) String() string {
	values := url.Values{}
	values.Set("address", l.Address)
	values.Set("token", l.Token)
	if l.Name != "" {
		values.Set("name", l.Name)
	}
	return Scheme + "://" + Host + "?" + values.Encode()
}

// Parse reads a link back. It is strict about the scheme and host and lenient
// about everything else, because the string has travelled through a chat app.
func Parse(raw string) (Link, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Link{}, fmt.Errorf("the link is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return Link{}, fmt.Errorf("that does not look like a pairing link: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, Scheme) {
		return Link{}, fmt.Errorf("a pairing link starts with %s://", Scheme)
	}
	if !strings.EqualFold(parsed.Host, Host) {
		return Link{}, fmt.Errorf("a pairing link looks like %s://%s followed by a query", Scheme, Host)
	}
	query := parsed.Query()
	link := Link{
		Address: strings.TrimSpace(query.Get("address")),
		Token:   strings.TrimSpace(query.Get("token")),
		Name:    strings.TrimSpace(query.Get("name")),
	}
	if link.Address == "" || link.Token == "" {
		return Link{}, fmt.Errorf("the link is missing its address or token")
	}
	return link, nil
}
