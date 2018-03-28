package vaultkv

//Get retrieves the secret at the given path and unmarshals it into the given
//output object. If the object is nil, an unmarshal will not be attempted (this
//can be used to check for existence). If the object could not be unmarshalled
//into, the resultant error is returned. Example path would be /secret/foo, if
//Key/Value backend were mounted at "/secret"
func (v *VaultKV) Get(path string, output interface{}) error {
	var unmarshalInto interface{}
	if output != nil {
		unmarshalInto = &vaultResponse{Data: &output}
	}

	err := v.doRequest("GET", path, nil, unmarshalInto)
	if err != nil {
		return err
	}

	return err
}

func (v *VaultKV) List(path string) ([]string, error) {
	ret := []string{}

	err := v.doRequest("LIST", path, nil, &vaultResponse{
		Data: struct {
			Keys *[]string `json:"keys"`
		}{
			Keys: &ret,
		},
	})
	if err != nil {
		return nil, err
	}

	return ret, err
}

func (v *VaultKV) Set(path string, values map[string]string) error {
	err := v.doRequest("PUT", path, &values, nil)
	if err != nil {
		return err
	}

	return nil
}

func (v *VaultKV) Delete(path string) error {
	err := v.doRequest("DELETE", path, nil, nil)
	if err != nil {
		return err
	}

	return nil
}
