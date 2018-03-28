package vaultkv

//Authenticator represents an object that can authenticate to the Vault
// or that represents authentication to the vault and hands back the
// access token required to put in Vault requests to signify authentication.
type Authenticator interface {
	//Token returns the access token held by this Authenticator
	Token() string
}
