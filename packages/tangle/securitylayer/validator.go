package securitylayer

import (
	"github.com/iotaledger/hive.go/crypto/ed25519"
	"github.com/iotaledger/hive.go/identity"
)

// region ValidatorID //////////////////////////////////////////////////////////////////////////////////////////////////

type ValidatorID = identity.ID

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Validator ////////////////////////////////////////////////////////////////////////////////////////////////////

type Validator struct {
	Events *ValidatorEvents

	identity *identity.Identity
	weight   float64
}

func NewValidator(publicKey ed25519.PublicKey, weight float64) (newValidator *Validator) {
	return &Validator{
		identity: identity.New(publicKey),
		weight:   weight,
	}
}

func (v *Validator) ID() (id ValidatorID) {
	return v.identity.ID()
}

func (v *Validator) PublicKey() (publicKey ed25519.PublicKey) {
	return v.identity.PublicKey()
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region ValidatorEvents //////////////////////////////////////////////////////////////////////////////////////////////

type ValidatorEvents struct{}

func NewValidatorEvents() (newValidatorEvents *ValidatorEvents) {
	return &ValidatorEvents{}
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Validators ///////////////////////////////////////////////////////////////////////////////////////////////////

type Validators map[ValidatorID]*Validator

func NewValidators(optionalValidators ...*Validator) (newValidators Validators) {
	newValidators = make(Validators)
	for _, validator := range optionalValidators {
		newValidators[validator.ID()] = validator
	}

	return newValidators
}

func (v Validators) Add(validator *Validator) (added bool) {
	if _, exists := v[validator.ID()]; exists {
		return false
	}

	v[validator.ID()] = validator
	return true
}

func (v Validators) Remove(validatorID ValidatorID) (removed bool) {
	if _, exists := v[validatorID]; !exists {
		return false
	}

	delete(v, validatorID)
	return true
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
