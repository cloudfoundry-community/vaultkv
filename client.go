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
	AuthToken string
	VaultURL  url.URL
}

type vaultResponse struct {
	Data interface{} `json:"data"`
	//There's totally more to the response, but this is all I care about atm.
}

func (v *VaultKV) doRequest(
	method, path string,
	input interface{},
	output interface{}) error {

	u := v.VaultURL
	u.Path = fmt.Sprintf("/v1/%s", strings.TrimPrefix(path, "/"))

	var body io.Reader
	if input != nil {
		body = &bytes.Buffer{}
		err := json.NewEncoder(body.(*bytes.Buffer)).Encode(input)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return err
	}

	req.Header.Add("X-Vault-Proto", v.AuthToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		err = v.parseError(resp)
		if err != nil {
			return err
		}
	}

	//If the status code is 204, there is no body. That leaves only 200.
	if output != nil && resp.StatusCode == 200 {
		err = json.NewDecoder(resp.Body).Decode(&output)
	}

	return err
}
