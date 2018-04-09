package vaultkv

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func (v *Client) doSysRequest(
	method, path string,
	input interface{},
	output interface{}) error {
	err := v.doRequest(method, path, input, output)
	//In sys contexts, 400 can mean that the Vault is uninitialized.
	if _, is400 := err.(*ErrBadRequest); is400 {
		initialized, err := v.IsInitialized()
		if err != nil {
			return err
		}

		if !initialized {
			return &ErrUninitialized{message: "Your Vault is not initialized"}
		}
	}

	return err
}

//IsInitialized returns true if the targeted Vault is initialized
func (v *Client) IsInitialized() (is bool, err error) {
	//Don't call doSysRequest from here because it calls IsInitialized
	// and that could get ugly
	err = v.doRequest(
		"GET",
		"/sys/init",
		nil,
		&struct {
			Initialized *bool `json:"initialized"`
		}{
			Initialized: &is,
		})

	return
}

type SealState struct {
	//Type is the type of unseal key. It is not returned from Unseal
	Type   string `json:"type,omitempty"`
	Sealed bool   `json:"sealed"`
	//Threshold is the number of keys required to reconstruct the master key
	Threshold int `json:"t"`
	//NumShares is the number of keys the master key has been split into
	NumShares int `json:"n"`
	//Progress is the number of keys that have been provided in the current unseal attempt
	Progress int    `json:"progress"`
	Nonce    string `json:"nonce"`
	Version  string `json:"version"`
	//ClusterName is only returned from unseal
	ClusterName string `json:"cluster_name,omitempty"`
	//ClusterID is only returned from unseal
	ClusterID string `json:"cluster_id,omitempty"`
}

//SealStatus calls the /sys/seal-status endpoint and returns the info therein
func (v *Client) SealStatus() (ret *SealState, err error) {
	err = v.doSysRequest(
		"GET",
		"/sys/seal-status",
		nil,
		&ret)

	return
}

type InitConfig struct {
	//Split the master key into this many shares
	Shares int `json:"secret_shares"`
	//This many shares are required to reconstruct the master key
	Threshold int `json:"secret_threshold"`
	//TODO: Add GPG stuff
}

type InitVaultOutput struct {
	client     *Client
	Keys       []string `json:"keys"`
	KeysBase64 []string `json:"keys_base64"`
	RootToken  string   `json:"root_token"`
}

func (i *InitVaultOutput) Unseal() error {
	for _, key := range i.Keys {
		sealState, err := i.client.Unseal(key)
		if err != nil {
			return err
		}

		if !sealState.Sealed {
			break
		}
	}

	return nil
}

//InitVault puts to the /sys/init endpoint to initialize the Vault, and returns
// the root token and unseal keys that were generated. The token of the client
// object is automatically set to the root token if the init is successful.
//If the vault has already been initialized, this returns *ErrBadRequest
func (v *Client) InitVault(in InitConfig) (out *InitVaultOutput, err error) {
	out = &InitVaultOutput{}
	err = v.doSysRequest(
		"PUT",
		"/sys/init",
		&in,
		&out,
	)

	if err == nil {
		v.AuthToken = out.RootToken
	}

	out.client = v

	return
}

//Seal puts to the /sys/seal endpoint to seal the Vault.
// If the Vault is already sealed, this doesn't return an error.
// If the Vault is unsealed and an incorrect token is provided, then this
// returns *ErrForbidden.
func (v *Client) Seal() error {
	return v.doSysRequest("PUT", "/sys/seal", nil, nil)
}

//Unseal puts to the /sys/unseal endpoint with a single key to progress the
//unseal attempt. If the unseal was successful, then the Sealed member of the
//returned struct will be false. If the given unseal key is improperly
//formatted, an *ErrBadRequest is returned. If the vault is already unsealed,
//no error is returned
func (v *Client) Unseal(key string) (out *SealState, err error) {
	out = &SealState{}
	err = v.doSysRequest(
		"PUT",
		"/sys/unseal",
		&struct {
			Key string `json:"key"`
		}{
			Key: key,
		},
		&out,
	)

	return
}

//ResetUnseal resets the current unseal attempt, such that the progress towards
//an unseal becomes 0. If the vault is unsealed, nothing happens and no error
//is returned.
func (v *Client) ResetUnseal() (err error) {
	err = v.doSysRequest(
		"PUT",
		"/sys/unseal",
		&struct {
			Reset bool `json:"reset"`
		}{
			Reset: true,
		},
		nil,
	)

	return
}

func (v *Client) Health(standbyok bool) error {
	//Don't call doRequest from Health because ParseError calls Health
	query := url.Values{}
	boolStr := "false"
	if standbyok == true {
		boolStr = "true"
	}
	query.Add("standbyok", boolStr)
	u := *v.VaultURL
	u.Path = "/v1/sys/health"
	u.RawQuery = query.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return err
	}

	req.Header.Add("X-Vault-Token", v.AuthToken)
	resp, err := v.Client.Do(req)
	if err != nil {
		return &ErrTransport{message: err.Error()}
	}

	errorsStruct := apiError{}
	json.NewDecoder(resp.Body).Decode(&errorsStruct)
	errorMessage := strings.Join(errorsStruct.Errors, "\n")

	switch resp.StatusCode {
	case 200:
		err = nil
	case 429:
		err = &ErrStandby{message: errorMessage}
	case 501:
		err = &ErrUninitialized{message: errorMessage}
	case 503:
		err = &ErrSealed{message: errorMessage}
	default:
		err = errors.New(errorMessage)
	}

	return err
}
