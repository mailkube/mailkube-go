package mailkube

import (
	"context"
	"net/http"
)

// credentialInfoPath is the route on which the calling key describes itself.
const credentialInfoPath = "api-keys/me"

// infoSpec builds the request describing the calling key.
func infoSpec() requestSpec {
	return requestSpec{path: credentialInfoPath, method: http.MethodGet}
}

// CredentialService is the namespace for the client's own credential, reached as
// client.Credential.
//
// It describes the API key the client was built with and holds no setter: a client takes its
// key in New and keeps it, which is what makes one client safe for concurrent use. A second key
// is a second client.
type CredentialService struct {
	transport jsonTransport
}

// Info describes the API key this client authenticates with: its domain, organization,
// source-IP allowlist, plan and send-quota usage.
//
// The key is checked exactly as a send checks it, so a key that cannot send fails with
// ErrAuthentication (invalid_api_key), which also makes Info the way to verify a key. The call
// counts as a use of the key. Plan and quota figures can be up to 30 seconds old.
func (s *CredentialService) Info(ctx context.Context) (*APIKey, error) {
	return fetch[APIKey](ctx, s.transport, infoSpec())
}
