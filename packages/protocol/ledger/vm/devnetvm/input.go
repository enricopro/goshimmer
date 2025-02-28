package devnetvm

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"

	"github.com/iotaledger/hive.go/core/generics/lo"
	"github.com/iotaledger/hive.go/core/generics/model"
	"github.com/iotaledger/hive.go/core/serix"
	"github.com/iotaledger/hive.go/core/stringify"
	"github.com/iotaledger/hive.go/core/types"
	"github.com/iotaledger/hive.go/core/typeutils"

	"github.com/iotaledger/goshimmer/packages/protocol/ledger/utxo"
)

func init() {
	err := serix.DefaultAPI.RegisterTypeSettings(UTXOInput{}, serix.TypeSettings{}.WithObjectType(uint8(new(UTXOInput).Type())))
	if err != nil {
		panic(fmt.Errorf("error registering UTXOInput type settings: %w", err))
	}
	err = serix.DefaultAPI.RegisterInterfaceObjects((*Input)(nil), new(UTXOInput))
	if err != nil {
		panic(fmt.Errorf("error registering Input interface implementations: %w", err))
	}
}

// region InputType ////////////////////////////////////////////////////////////////////////////////////////////////////

const (
	// UTXOInputType is the type of an Input that references an UTXO Output.
	UTXOInputType InputType = iota

	// MinInputCount defines the minimum amount of Inputs in a Transaction.
	MinInputCount = 1

	// MaxInputCount defines the maximum amount of Inputs in a Transaction.
	MaxInputCount = 127
)

// InputType represents the type of an Input.
type InputType uint8

// InputTypeNames contains the names of the existing InputTypes.
var InputTypeNames = [...]string{
	"UTXOInputType",
}

// String returns a human readable representation of the InputType.
func (i InputType) String() string {
	if i > InputType(len(InputTypeNames)-1) {
		return fmt.Sprintf("InputType(%X)", uint8(i))
	}

	return InputTypeNames[i]
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Input ////////////////////////////////////////////////////////////////////////////////////////////////////////

// Input is a generic interface for different kinds of Inputs.
type Input interface {
	// Type returns the type of the Input.
	Type() InputType

	// Bytes returns a marshaled version of the Input.
	Bytes() ([]byte, error)

	// String returns a human readable version of the Input.
	String() string

	// Base58 returns the base58 encoded input.
	Base58() string

	// Compare offers a comparator for Inputs which returns -1 if other Input is bigger, 1 if it is smaller and 0 if they
	// are the same.
	Compare(other Input) int
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region Inputs ///////////////////////////////////////////////////////////////////////////////////////////////////////

// Inputs represents a collection of Inputs that ensures a deterministic order.
type Inputs []Input

// NewInputs returns a deterministically ordered collection of Inputs removing existing duplicates.
func NewInputs(optionalInputs ...Input) (inputs Inputs) {
	seenInputs := make(map[string]types.Empty)
	sortedInputs := make([]struct {
		input           Input
		inputSerialized []byte
	}, 0)

	// filter duplicates (store marshaled version so we don't need to marshal a second time during sort)
	for _, input := range optionalInputs {
		marshaledInput := lo.PanicOnErr(input.Bytes())
		marshaledInputAsString := typeutils.BytesToString(marshaledInput)

		if _, seenAlready := seenInputs[marshaledInputAsString]; seenAlready {
			continue
		}
		seenInputs[marshaledInputAsString] = types.Void

		sortedInputs = append(sortedInputs, struct {
			input           Input
			inputSerialized []byte
		}{input, marshaledInput})
	}
	// TODO: does it need to be sorted here? Is ordering in serialized form enough?
	// sort inputs
	sort.Slice(sortedInputs, func(i, j int) bool {
		return bytes.Compare(sortedInputs[i].inputSerialized, sortedInputs[j].inputSerialized) < 0
	})

	// create result
	inputs = make(Inputs, len(sortedInputs))
	for i, sortedInput := range sortedInputs {
		inputs[i] = sortedInput.input
	}

	return
}

// Clone creates a copy of the Inputs.
func (i Inputs) Clone() (clonedInputs Inputs) {
	clonedInputs = make(Inputs, len(i))
	copy(clonedInputs[:], i)

	return
}

// String returns a human-readable version of the Inputs.
func (i Inputs) String() string {
	structBuilder := stringify.NewStructBuilder("Inputs")
	for i, input := range i {
		structBuilder.AddField(stringify.NewStructField(strconv.Itoa(i), input))
	}

	return structBuilder.String()
}

// Strings returns the Inputs in the form []transactionID:index.
func (i Inputs) Strings() (result []string) {
	for _, input := range i {
		if input.Type() == UTXOInputType {
			outputID := input.(*UTXOInput).ReferencedOutputID()
			result = append(result, fmt.Sprintf("%s", outputID))
		}
	}

	return
}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////

// region UTXOInput ////////////////////////////////////////////////////////////////////////////////////////////////////

// UTXOInput represents a reference to an Output in the UTXODAG.
type UTXOInput struct {
	model.Immutable[UTXOInput, *UTXOInput, utxoInputModel] `serix:"0"`
}
type utxoInputModel struct {
	ReferencedOutputID utxo.OutputID `serix:"0"`
}

// NewUTXOInput is the constructor for UTXOInputs.
func NewUTXOInput(referencedOutputID utxo.OutputID) *UTXOInput {
	return model.NewImmutable[UTXOInput](&utxoInputModel{
		ReferencedOutputID: referencedOutputID,
	})
}

// Type returns the type of the Input.
func (u *UTXOInput) Type() InputType {
	return UTXOInputType
}

// ReferencedOutputID returns the OutputID that this Input references.
func (u *UTXOInput) ReferencedOutputID() utxo.OutputID {
	return u.M.ReferencedOutputID
}

// Base58 returns the base58 encoded referenced output ID of this input.
func (u *UTXOInput) Base58() string {
	return u.M.ReferencedOutputID.Base58()
}

// Compare offers a comparator for Inputs which returns -1 if other Input is bigger, 1 if it is smaller and 0 if they
// are the same.
func (u *UTXOInput) Compare(other Input) int {
	return bytes.Compare(lo.PanicOnErr(u.Bytes()), lo.PanicOnErr(other.Bytes()))
}

// code contract (make sure the struct implements all required methods)
var _ Input = &UTXOInput{}

// endregion ///////////////////////////////////////////////////////////////////////////////////////////////////////////
