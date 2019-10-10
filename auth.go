package vaultkv

import (
	"fmt"
	"time"
)

//AuthOutput is the general structure as returned by AuthX functions. The
//Metadata member type is determined by the specific Auth function. Note that
//the Vault must be initialized and unsealed in order to use authentication
//endpoints.
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

//AuthGithubMetadata is the metadata member set by AuthGithub.
type AuthGithubMetadata struct {
	Username     string `json:"username"`
	Organization string `json:"org"`
}

//AuthGithub submits the given accessToken to the github auth endpoint, checking
// it against configurations for Github organizations. If the accessToken
// belongs to an authorized account, then the AuthOutput object is returned, and
// this client's AuthToken is set to the returned token.
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

//AuthLDAPMetadata is the metadata member set by AuthLDAP
type AuthLDAPMetadata struct {
	Username string `json:"username"`
}

//AuthLDAP submits the given username and password to the LDAP auth endpoint,
//checking it against existing LDAP auth configurations. If auth is successful,
//then the AuthOutput object is returned, and this client's AuthToken is set to
//the returned token.
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

//AuthUserpassMetadata is the metadata member set by AuthUserpass
type AuthUserpassMetadata struct {
	Username string `json:"username"`
}

//AuthUserpass submits the given username and password to the userpass auth
//endpoint. If a username with that password exists, then the AuthOutput object
//is returned, and this client's AuthToken is set to the returned token.
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

func (v *Client) AuthApprole(roleID, secretID string) (ret *AuthOutput, err error) {
	ret = &AuthOutput{}
	err = v.doRequest(
		"POST",
		"/auth/approle/login",
		struct {
			RoleID   string `json:"role_id"`
			SecretID string `json:"secret_id"`
		}{
			RoleID:   roleID,
			SecretID: secretID,
		},
		&ret,
	)

	if err == nil {
		v.AuthToken = ret.Auth.ClientToken
	}

	return
}

//TokenRenewSelf takes the token in the Client object and attempts to renew its
// lease.
func (v *Client) TokenRenewSelf() (err error) {
	return v.doRequest("POST", "/auth/token/renew-self", nil, nil)
}

type TokenInfo struct {
	Accessor       string
	CreationTime   time.Time
	CreationTTL    time.Duration
	DisplayName    string
	EntityID       string
	ExpireTime     time.Time
	ExplicitMaxTTL time.Duration
	ID             string
	IssueTime      time.Time
	NumUses        int64
	Orphan         bool
	Path           string
	Policies       []string
	Renewable      bool
	TTL            time.Duration
}

type tokenInfoRaw struct {
	Accessor       string   `json:"accessor"`
	CreationTime   int64    `json:"creation_time"`
	CreationTTL    int64    `json:"creation_ttl"`
	DisplayName    string   `json:"display_name"`
	EntityID       string   `json:"entity_id"`
	ExpireTime     string   `json:"expire_time"`
	ExplicitMaxTTL int64    `json:"explicit_max_ttl"`
	ID             string   `json:"id"`
	IssueTime      string   `json:"issue_time"`
	NumUses        int64    `json:"num_uses"`
	Orphan         bool     `json:"orphan"`
	Path           string   `json:"path"`
	Policies       []string `json:"policies"`
	Renewable      bool     `json:"renewable"`
	TTL            int64    `json:"ttl"`
}

//TokenInfoSelf returns the contents of the token self info endpoint of the vault
func (v *Client) TokenInfoSelf() (ret *TokenInfo, err error) {
	raw := tokenInfoRaw{}
	err = v.doRequest("GET", "/auth/token/lookup-self", nil, &raw)
	if err != nil {
		return
	}

	expTime, err := time.Parse(time.RFC3339Nano, raw.ExpireTime)
	if err != nil {
		return
	}

	issTime, err := time.Parse(time.RFC3339Nano, raw.IssueTime)
	if err != nil {
		return
	}

	ret = &TokenInfo{
		Accessor:       raw.Accessor,
		CreationTime:   time.Unix(raw.CreationTime, 0),
		CreationTTL:    time.Duration(raw.CreationTTL) * time.Second,
		DisplayName:    raw.DisplayName,
		EntityID:       raw.EntityID,
		ExpireTime:     expTime,
		ExplicitMaxTTL: time.Duration(raw.ExplicitMaxTTL) * time.Second,
		ID:             raw.ID,
		IssueTime:      issTime,
		NumUses:        raw.NumUses,
		Orphan:         raw.Orphan,
		Path:           raw.Path,
		Policies:       raw.Policies,
		Renewable:      raw.Renewable,
		TTL:            time.Duration(raw.TTL) * time.Second,
	}

	return
}

//TokenIsValid returns no error if it can look itself up. This can error
// if the token is valid but somebody has configured policies such that it can not
// look itself up. It can also error, of course, if the token is invalid.
func (v *Client) TokenIsValid() (err error) {
	return v.doRequest("GET", "/auth/token/lookup-self", nil, nil)
}
