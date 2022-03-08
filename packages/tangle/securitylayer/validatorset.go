package securitylayer

import (
	"sync"

	"github.com/iotaledger/hive.go/events"
)

type ValidatorSet struct {
	Events *ValidatorSetEvents

	validators      Validators
	validatorsMutex sync.RWMutex
}

func NewValidatorSet(optionalValidators ...*Validator) (newValidatorSet *ValidatorSet) {
	newValidatorSet = &ValidatorSet{
		Events:     NewValidatorSetEvents(),
		validators: NewValidators(optionalValidators...),
	}

	return newValidatorSet
}

func (v *ValidatorSet) Add(validator *Validator) (added bool) {
	defer func() {
		if added {
			v.Events.ValidatorAdded.Trigger(validator)
		}
	}()

	v.validatorsMutex.Lock()
	defer v.validatorsMutex.Unlock()

	added = v.validators.Add(validator)

	return
}

type ValidatorSetEvents struct {
	ValidatorAdded   *events.Event
	ValidatorRemoved *events.Event
}

func NewValidatorSetEvents() *ValidatorSetEvents {
	return &ValidatorSetEvents{
		ValidatorAdded: events.NewEvent(func(handler interface{}, params ...interface{}) {
			handler.(func(validator *Validator))(params[0].(*Validator))
		}),
		ValidatorRemoved: events.NewEvent(func(handler interface{}, params ...interface{}) {
			handler.(func(validator *Validator))(params[0].(*Validator))
		}),
	}
}
