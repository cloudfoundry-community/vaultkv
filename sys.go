package vaultkv

func (v *VaultKV) doSysRequest(
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
			return &ErrUninitialized{message: err.Error()}
		}
	}

	return err
}

//IsInitialized returns true if the targeted Vault is initialized
func (v *VaultKV) IsInitialized() (is bool, err error) {
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
	//Type is not returned from
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
func (v *VaultKV) SealStatus() (ret *SealState, err error) {
	err = v.doSysRequest(
		"GET",
		"/sys/seal-status",
		nil,
		ret)

	return
}

type InitVaultInput struct {
	//Split the master key into this many shares
	SecretShares int `json:"secret_shares"`
	//This many shares are required to reconstruct the master key
	SecretThreshold int `json:"secret_threshold"`
}

type InitVaultOutput struct {
	Keys       []string `json:"keys"`
	KeysBase64 []string `json:"keys_base64"`
	RootToken  string   `json:"root_token"`
}

//InitVault puts to the /sys/init endpoint to initialize the Vault, and returns
func (v *VaultKV) InitVault(in InitVaultInput) (out *InitVaultOutput, err error) {
	err = v.doSysRequest(
		"PUT",
		"/sys/init",
		&in,
		out,
	)
	return
}

//Seal puts to the /sys/seal endpoint to seal the Vault.
func (v *VaultKV) Seal() error {
	return v.doSysRequest("PUT", "/sys/seal", nil, nil)
}

//Unseal puts to the /sys/unseal endpoint with a single key to progress the
// unseal attempt. If the unseal was successful, then the Sealed member of the
// returned struct will be false
func (v *VaultKV) Unseal(key string) (out *SealState, err error) {
	err = v.doSysRequest(
		"PUT",
		"/sys/unseal",
		&struct {
			Key string `json:"key"`
		}{
			Key: key,
		},
		out,
	)

	return
}
