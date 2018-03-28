package vaultkv

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

//ErrBadRequest represents 400 status codes that are returned from the API.
//See: your fault.
type ErrBadRequest struct {
	message string
}

func (e *ErrBadRequest) Error() string {
	return e.message
}

//ErrForbidden represents 403 status codes returned from the API. This could be
// if your auth is wrong or expired, or you simply don't have access to do the
// particular thing you're trying to do. Check your privilege.
type ErrForbidden struct {
	message string
}

func (e *ErrForbidden) Error() string {
	return e.message
}

//ErrNotFound represents 404 status codes returned from the API. This could be
// either that the thing you're looking for doesn't exist, or in some cases
// that you don't have access to the thing you're looking for and that Vault is
// hiding it from you.
type ErrNotFound struct {
	message string
}

func (e *ErrNotFound) Error() string {
	return e.message
}

//ErrInternalServer represents 500 status codes that are returned from the API.
//See: their fault.
type ErrInternalServer struct {
	message string
}

func (e *ErrInternalServer) Error() string {
	return e.message
}

//ErrSealed represents the 503 status code that is returned by Vault most
// commonly if the Vault is currently sealed, but could also represent the Vault
// being in a maintenance state.
type ErrSealed struct {
	message string
}

func (e *ErrSealed) Error() string {
	return e.message
}

//ErrUninitialized represents a 503 status code being returned and the Vault
//being uninitialized.
type ErrUninitialized struct {
	message string
}

func (e *ErrUninitialized) Error() string {
	return e.message
}

func (v *VaultKV) parseError(r *http.Response) (err error) {
	errorsStruct := struct {
		Errors []string `json:"errors"`
	}{}

	json.NewDecoder(r.Body).Decode(&errorsStruct)
	errorMessage := strings.Join(errorsStruct.Errors, "\n")

	switch r.StatusCode {
	case 400:
		err = &ErrBadRequest{message: errorMessage}
	case 403:
		err = &ErrForbidden{message: errorMessage}
	case 404:
		err = &ErrNotFound{message: errorMessage}
	case 500:
		err = &ErrInternalServer{message: errorMessage}
	case 503:
		err = v.parse503(errorMessage)
	default:
		err = errors.New(errorMessage)
	}

	return
}

func (v *VaultKV) parse503(message string) (err error) {
	initialized, err := v.IsInitialized()
	if err != nil {
		return
	}
	if !initialized {
		return &ErrUninitialized{message: message}
	}

	sealState, err := v.SealStatus()
	if err != nil {
		return
	}
	if sealState.Sealed {
		return &ErrSealed{message: message}
	}

	return errors.New(message)
}
