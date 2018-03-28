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

func parseError(r *http.Response) (err error) {
	formatError := func() string {
		errorsStruct := struct {
			Errors []string `json:"errors"`
		}{}

		json.NewDecoder(r.Body).Decode(&errorsStruct)
		return strings.Join(errorsStruct.Errors, "\n")
	}

	defer r.Body.Close()

	switch r.StatusCode {
	case 400:
		err = &ErrBadRequest{message: formatError()}
	case 403:
		err = &ErrForbidden{message: formatError()}
	case 404:
		err = &ErrNotFound{message: formatError()}
	case 500:
		err = &ErrInternalServer{message: formatError()}
	case 503:
		err = &ErrSealed{message: formatError()}
	default:
		err = errors.New(formatError())
	}

	return
}
