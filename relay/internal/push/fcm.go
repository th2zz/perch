package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrNoCredentials means the gateway has no service account and therefore
// cannot sign anything.
var ErrNoCredentials = errors.New("no FCM service account configured")

// ServiceAccount is the subset of a Google service-account key we need.
type ServiceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// ParseServiceAccount reads a service-account JSON key.
func ParseServiceAccount(data []byte) (*ServiceAccount, error) {
	var sa ServiceAccount
	if err := json.Unmarshal(data, &sa); err != nil {
		return nil, fmt.Errorf("service account is not valid JSON: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, errors.New("service account is missing project_id, client_email or private_key")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &sa, nil
}

// FCMSender posts messages to FCM HTTP v1.
//
// The OAuth2 dance is implemented with the standard library on purpose: the
// gateway must be a single self-contained binary that a user could audit, and
// the whole exchange is one signed JWT and one token request.
type FCMSender struct {
	Account *ServiceAccount
	Client  *http.Client

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewFCMSender builds a sender, or returns ErrNoCredentials when unconfigured
// so the caller can decide between failing and running in dry-run mode.
func NewFCMSender(keyJSON []byte) (*FCMSender, error) {
	if len(keyJSON) == 0 {
		return nil, ErrNoCredentials
	}
	sa, err := ParseServiceAccount(keyJSON)
	if err != nil {
		return nil, err
	}
	return &FCMSender{Account: sa, Client: &http.Client{Timeout: 15 * time.Second}}, nil
}

// ProjectID names the FCM project, for logging and URL building.
func (f *FCMSender) ProjectID() string { return f.Account.ProjectID }

// Send delivers one notification.
func (f *FCMSender) Send(ctx context.Context, deviceToken string, n Notification) error {
	token, err := f.token(ctx)
	if err != nil {
		return err
	}

	// android.priority=high is what lets the message through Doze; the channel
	// id is what the app uses to keep decision notifications in their own
	// bucket with their own importance.
	message := map[string]any{
		"message": map[string]any{
			"token":        deviceToken,
			"notification": map[string]string{"title": n.Title, "body": n.Body},
			"data":         n.Data,
			"android": map[string]any{
				"priority": "high",
				"notification": map[string]any{
					"channel_id": "perch_decisions",
				},
			},
		},
	}
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}

	endpoint := "https://fcm.googleapis.com/v1/projects/" + f.Account.ProjectID + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := f.Client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("fcm returned %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

// token returns a cached access token, refreshing it before expiry.
func (f *FCMSender) token(ctx context.Context) (string, error) {
	f.mu.Lock()
	if f.accessToken != "" && time.Now().Before(f.expiresAt) {
		token := f.accessToken
		f.mu.Unlock()
		return token, nil
	}
	f.mu.Unlock()

	signed, err := f.signJWT()
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", signed)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.Account.TokenURI,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := f.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("token endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var parsed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.AccessToken == "" {
		return "", errors.New("token endpoint returned no access_token")
	}

	f.mu.Lock()
	f.accessToken = parsed.AccessToken
	// Renew a minute early so an in-flight send never uses an expired token.
	f.expiresAt = time.Now().Add(time.Duration(parsed.ExpiresIn)*time.Second - time.Minute)
	f.mu.Unlock()
	return parsed.AccessToken, nil
}

func (f *FCMSender) signJWT() (string, error) {
	key, err := parsePrivateKey(f.Account.PrivateKey)
	if err != nil {
		return "", err
	}
	now := time.Now()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":   f.Account.ClientEmail,
		"scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud":   f.Account.TokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := b64(hb) + "." + b64(cb)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + b64(sig), nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func parsePrivateKey(pemKey string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("private_key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private_key is not an RSA key")
		}
		return rsaKey, nil
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse private_key: %w", err)
	}
	return key, nil
}
