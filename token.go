package vaultkv

import "net/url"

//TokenAuth is an implementation of Authenticator with auth tokens
type TokenAuth struct {
	//AuthToken is the actual token used to auth with.
	AuthToken string
	VaultURL  *url.URL
}

//Token returns the token configured with this TokenAuth object.
func (t *TokenAuth) Token() string {
	return t.AuthToken
}
