package anchor

import (
	"errors"
	"fmt"
	"sync"

	"github.com/iotaledger/goshimmer/packages/tangle/payload"
	"github.com/iotaledger/hive.go/marshalutil"
	"github.com/iotaledger/hive.go/stringify"
	"github.com/mr-tron/base58"
)

// Name				Type			Description
// Size				uint32			Size of the payload
// Payload Type		uint32			Set to value 9 to denote a Stamp from child
// Version			uint8			The version of the payload
// ChildTangleID	ByteArray[32]
// LastStampID		ByteArray[32]	This is the id of a message from the parent Tangle, which is the last stamp from the child Tangle ´Child Tangle Id´
// ChildMessageID	ByteArray[32]	This is the id of a message from the child Tangle ´Child Tangle Id´, which is the message to be anchored in the parent Tangle
// Merkle Root		ByteArray[32]	The Merkle root of the set of messages below
// Signature		ByteArray[?]	Signature of the issuing node

const (
	// PayloadName defines the name of the anchor payload.
	PayloadName = "anchor"
	payloadType = 889
)

// Payload represents the anchor payload type.
type Payload struct {
	Version        uint8
	ChildTangleID  []byte
	LastStampID    []byte
	ChildMessageID []byte
	MerkleRoot     []byte

	bytes      []byte
	bytesMutex sync.RWMutex
}

// NewPayload creates a new anchor payload.
func NewPayload(version uint8, childTangleID, lastStampID, childMessageID, merkleRoot []byte) *Payload {
	return &Payload{
		Version:        version,
		ChildTangleID:  childMessageID,
		LastStampID:    lastStampID,
		ChildMessageID: childMessageID,
		MerkleRoot:     merkleRoot,
	}
}

// FromBytes parses the marshaled version of a Payload into a Go object.
// It either returns a new Payload or fills an optionally provided Payload with the parsed information.
func FromBytes(bytes []byte) (result *Payload, consumedBytes int, err error) {
	marshalUtil := marshalutil.New(bytes)
	result, err = Parse(marshalUtil)
	consumedBytes = marshalUtil.ReadOffset()

	return
}

// Parse unmarshals an Payload using the given marshalUtil (for easier marshaling/unmarshaling).
func Parse(marshalUtil *marshalutil.MarshalUtil) (result *Payload, err error) {
	// read information that are required to identify the payloa from the outside
	if _, err = marshalUtil.ReadUint32(); err != nil {
		err = fmt.Errorf("failed to parse payload size of anchor payload: %w", err)
		return
	}
	if _, err = marshalUtil.ReadUint32(); err != nil {
		err = fmt.Errorf("failed to parse payload type of anchor payload: %w", err)
		return
	}

	result = &Payload{}

	version, err := marshalUtil.ReadUint8()
	if err != nil {
		err = fmt.Errorf("failed to parse version field of anchor payload: %w", err)
		return
	}
	result.Version = version

	childTangleID, err := marshalUtil.ReadBytes(32)
	if err != nil {
		err = fmt.Errorf("failed to parse childTangleID field of anchor payload: %w", err)
		return
	}
	result.ChildTangleID = childTangleID

	lastStampID, err := marshalUtil.ReadBytes(32)
	if err != nil {
		err = fmt.Errorf("failed to parse lastStampID field of chat payload: %w", err)
		return
	}
	result.LastStampID = lastStampID

	childMessageID, err := marshalUtil.ReadBytes(32)
	if err != nil {
		err = fmt.Errorf("failed to parse childMessageID field of chat payload: %w", err)
		return
	}
	result.ChildMessageID = childMessageID

	merkleRoot, err := marshalUtil.ReadBytes(32)
	if err != nil {
		err = fmt.Errorf("failed to parse merkleRoot field of chat payload: %w", err)
		return
	}
	result.MerkleRoot = merkleRoot

	// store bytes, so we don't have to marshal manually
	consumedBytes := marshalUtil.ReadOffset()
	copy(result.bytes, marshalUtil.Bytes()[:consumedBytes])

	return result, nil
}

// Bytes returns a marshaled version of this Payload.
func (p *Payload) Bytes() (bytes []byte) {
	// acquire lock for reading bytes
	p.bytesMutex.RLock()

	// return if bytes have been determined already
	if bytes = p.bytes; bytes != nil {
		p.bytesMutex.RUnlock()
		return
	}

	// switch to write lock
	p.bytesMutex.RUnlock()
	p.bytesMutex.Lock()
	defer p.bytesMutex.Unlock()

	// return if bytes have been determined in the mean time
	if bytes = p.bytes; bytes != nil {
		return
	}

	payloadLength := int(marshalutil.Uint8Size + 32*4)
	// initialize helper
	marshalUtil := marshalutil.New(marshalutil.Uint32Size + marshalutil.Uint32Size + payloadLength)

	// marshal the payload specific information
	marshalUtil.WriteUint32(payload.TypeLength + uint32(payloadLength))
	marshalUtil.WriteBytes(Type.Bytes())
	marshalUtil.WriteUint8(p.Version)
	marshalUtil.WriteBytes(p.ChildTangleID[:32])
	marshalUtil.WriteBytes(p.LastStampID[:32])
	marshalUtil.WriteBytes(p.ChildMessageID[:32])
	marshalUtil.WriteBytes(p.MerkleRoot[:32])

	bytes = marshalUtil.Bytes()

	return bytes
}

// String returns a human-friendly representation of the Payload.
func (p *Payload) String() string {
	return stringify.Struct("AnchorPayload",
		stringify.StructField("version", p.Version),
		stringify.StructField("ChildTangleID", base58.Encode(p.ChildTangleID)),
		stringify.StructField("LastStampID", base58.Encode(p.LastStampID)),
		stringify.StructField("ChildMessageID", base58.Encode(p.ChildMessageID)),
		stringify.StructField("MerkleRoot", base58.Encode(p.MerkleRoot)),
	)
}

// Type represents the identifier which addresses the anchor payload type.
var Type = payload.NewType(payloadType, PayloadName, func(data []byte) (payload payload.Payload, err error) {
	var consumedBytes int
	payload, consumedBytes, err = FromBytes(data)
	if err != nil {
		return nil, err
	}
	if consumedBytes != len(data) {
		return nil, errors.New("not all payload bytes were consumed")
	}
	return
})

// Type returns the type of the Payload.
func (p *Payload) Type() payload.Type {
	return Type
}
