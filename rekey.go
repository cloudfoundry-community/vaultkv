package vaultkv

import "encoding/json"

type RekeyState struct {
	Started          bool   `json:"started"`
	Nonce            string `json:"nonce"`
	PendingThreshold int    `json:"t"`
	PendingShares    int    `json:"n"`
	//The number of keys given so far in this rekey operation
	Progress int `json:"progress"`
	//The total number of keys needed for this rekey operation
	Required        int      `json:"required"`
	PGPFingerprints []string `json:"pgp_fingerprints"`
	Backup          bool     `json:"backup"`
	//These come in after rekey completion
	Complete   bool     `json:"complete"`
	Keys       []string `json:"`
	KeysBase64 []string
}

func (v *Client) RekeyStatus() (ret *RekeyState, err error) {
	err = v.doSysRequest("GET", "/sys/rekey/init", nil, &ret)
	return ret, err
}

type RekeyStartInput struct {
	Shares    int      `json:"secret_shares"`
	Threshold int      `json:"secret_threshold`
	PGPKeys   []string `json:"pgp_keys"`
	Backup    bool     `json:"backup"`
}

func (v *Client) RekeyStart(input RekeyStartInput) error {
	return v.doSysRequest("PUT", "/sys/rekey/init", &input, nil)
}

func (v *Client) RekeyCancel() error {
	return v.doSysRequest("DELETE", "/sys/rekey/init", nil, nil)
}

type RekeyKeys struct {
	Keys            []string `json:"keys"`
	KeysBase64      []string `json:"keys_base64"`
	PGPFingerprints []string `json:"pgp_fingerprints"`
	Backup          bool     `json:"backup"`
}

//RekeySubmit takes an unseal key and the nonce of the current rekey operation
//and submits it to Vault. If an error occurs, only err will be non-nil. If the
//rekey is still in progress after the submission, only state will be non-nil.
//If the rekey was successful, then only keys will be non-nil.
func (v *Client) RekeySubmit(key string, nonce string) (state *RekeyState, keys *RekeyKeys, err error) {
	tempMap := make(map[string]interface{})
	err = v.doSysRequest(
		"PUT",
		"/sys/rekey/update",
		&struct {
			Key   string `json:"key"`
			Nonce string `json:"nonce"`
		}{
			Key:   key,
			Nonce: nonce,
		},
		&tempMap,
	)
	if err != nil {
		return
	}

	jBytes, err := json.Marshal(&tempMap)
	if err != nil {
		return
	}

	var unmarshalTarget interface{} = state
	if _, isComplete := tempMap["complete"]; isComplete {
		unmarshalTarget = keys
	}

	err = json.Unmarshal(jBytes, &unmarshalTarget)
	if err != nil {
		return
	}

	return
}
