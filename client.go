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

type Client struct {
	AuthToken string
	VaultURL  *url.URL
	//If Client is nil, http.DefaultClient will be used
	Client *http.Client
}

type vaultResponse struct {
	Data interface{} `json:"data"`
	//There's totally more to the response, but this is all I care about atm.
}

func (v *Client) doRequest(
	method, path string,
	input interface{},
	output interface{}) error {

	u := v.VaultURL
	u.Path = fmt.Sprintf("/v1/%s", strings.TrimPrefix(path, "/"))

	var body io.Reader
	if input != nil {
		if strings.ToUpper(method) == "GET" {
			//Input has to be a url.Values
			u.RawQuery = input.(url.Values).Encode()
		} else {
			body = &bytes.Buffer{}
			err := json.NewEncoder(body.(*bytes.Buffer)).Encode(input)
			if err != nil {
				return err
			}
		}
	}

	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return err
	}

	token := v.AuthToken
	if token == "" {
		token = "01234567-89ab-cdef-0123-456789abcdef"
	}

	req.Header.Add("X-Vault-Token", v.AuthToken)

	client := v.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return &ErrTransport{message: err.Error()}
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
