package securitylayer

import (
	"fmt"
	"testing"

	"github.com/iotaledger/hive.go/events"
	"github.com/iotaledger/hive.go/identity"
)

func TestValidatorSet_Add(t *testing.T) {
	validatorSet := NewValidatorSet()
	validatorSet.Events.ValidatorAdded.Attach(events.NewClosure(func(validator *Validator) {
		fmt.Println(validator)
	}))

	validator1 := NewValidator(*identity.GenerateIdentity(), 10)
	validator2 := NewValidator(*identity.GenerateIdentity(), 20)

	validatorSet.Add(validator1)
	validatorSet.Add(validator1)
	fmt.Println("==============")
	validatorSet.Add(validator2)
}
