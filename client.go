package vaultkv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type VaultKV struct {
	Auth     Authenticator
	VaultURL url.URL
}

type vaultResponse struct {
	Data interface{} `json:"data"`
	//There's totally more to the response, but this is all I care about atm.
}

func (v *VaultKV) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	u := v.VaultURL
	u.Path = fmt.Sprintf("/v1/%s", strings.TrimPrefix(path, "/"))

	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, err
	}

	req.Header.Add("X-Vault-Proto", v.Auth.Token())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		err = parseError(resp)
	}

	return resp, err
}

//Get retrieves the secret at the given path and unmarshals it into the given
//output object. If the object is nil, an unmarshal will not be attempted (this
//can be used to check for existence). If the object could not be unmarshalled
//into, the resultant error is returned. Example path would be /secret/foo, if
//Key/Value backend were mounted at "/secret"
func (v *VaultKV) Get(path string, output interface{}) error {
	resp, err := v.doRequest("GET", path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if output != nil {
		err = json.NewDecoder(resp.Body).Decode(vaultResponse{Data: &output})
	}

	return err
}

func (v *VaultKV) List(path string) ([]string, error) {
	resp, err := v.doRequest("LIST", path, nil)
	if err != nil {
		return nil, err
	}

	ret := []string{}
	err = json.NewDecoder(resp.Body).Decode(vaultResponse{
		Data: struct {
			Keys *[]string `json:"keys"`
		}{
			Keys: &ret,
		},
	})

	return ret, err
}

func (v *VaultKV) Set(path string, values map[string]string) error {
	body, err := json.Marshal(&values)
	if err != nil {
		return err
	}

	resp, err := v.doRequest("PUT", path, bytes.NewReader(body))
	if err != nil {
		return err
	}

	resp.Body.Close()
	return nil
}

func (v *VaultKV) Delete(path string) error {
	resp, err := v.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}

	resp.Body.Close()
	return nil
}
