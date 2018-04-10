//Package vaultkv provides a client with functions that make API calls that a user of
// Vault may commonly want.
package vaultkv

import "fmt"

type AuthOutput struct {
	LeaseID       string `json:"lease_id"`
	Renewable     bool   `json:"renewable"`
	LeaseDuration int    `json:"lease_duration"`
	Auth          struct {
		ClientToken string   `json:"client_token"`
		Accessor    string   `json:"accessor"`
		Policies    []string `json:"policies"`
	}
	//Metadata's internal structure is dependent on the auth type
	Metadata interface{} `json:"metadata"`
}

type AuthGithubMetadata struct {
	Username     string `json:"username"`
	Organization string `json:"org"`
}

func (v *Client) AuthGithub(accessToken string) (ret *AuthOutput, err error) {
	ret = &AuthOutput{Metadata: AuthGithubMetadata{}}
	err = v.doRequest(
		"POST",
		"/auth/github/login",
		struct {
			Token string `json:"token"`
		}{Token: accessToken},
		&ret,
	)

	if err == nil {
		v.AuthToken = ret.Auth.ClientToken
	}

	return
}

type AuthLDAPMetadata struct {
	Username string `json:"username"`
}

func (v *Client) AuthLDAP(username, password string) (ret *AuthOutput, err error) {
	ret = &AuthOutput{Metadata: AuthLDAPMetadata{}}
	err = v.doRequest(
		"POST",
		fmt.Sprintf("/auth/ldap/login/%s", username),
		struct {
			Password string `json:"password"`
		}{Password: password},
		&ret,
	)

	if err == nil {
		v.AuthToken = ret.Auth.ClientToken
	}

	return
}

type AuthUserpassMetadata struct {
	Username string `json:"username"`
}

func (v *Client) AuthUserpass(username, password string) (ret *AuthOutput, err error) {
	ret = &AuthOutput{Metadata: AuthUserpassMetadata{}}
	err = v.doRequest(
		"POST",
		fmt.Sprintf("/auth/userpass/login/%s", username),
		struct {
			Password string `json:"password"`
		}{Password: password},
		&ret,
	)

	if err == nil {
		v.AuthToken = ret.Auth.ClientToken
	}

	return
}
