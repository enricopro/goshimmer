package wallet

import (
	"github.com/iotaledger/hive.go/core/types/confirmation"

	"github.com/iotaledger/goshimmer/client/wallet/packages/address"
	"github.com/iotaledger/goshimmer/packages/protocol/chain/engine/congestioncontrol/icca/mana"
	"github.com/iotaledger/goshimmer/packages/protocol/chain/ledger/utxo"
	devnetvm2 "github.com/iotaledger/goshimmer/packages/protocol/chain/ledger/vm/devnetvm"
)

// Connector represents an interface that defines how the wallet interacts with the network. A wallet can either be used
// locally on a server or it can connect remotely using the web API.
type Connector interface {
	UnspentOutputs(addresses ...address.Address) (unspentOutputs OutputsByAddressAndOutputID, err error)
	SendTransaction(transaction *devnetvm2.Transaction) (err error)
	RequestFaucetFunds(address address.Address, powTarget int) (err error)
	GetAllowedPledgeIDs() (pledgeIDMap map[mana.Type][]string, err error)
	GetTransactionConfirmationState(txID utxo.TransactionID) (confirmationState confirmation.State, err error)
	GetUnspentAliasOutput(address *devnetvm2.AliasAddress) (output *devnetvm2.AliasOutput, err error)
}
