package jsonmodels

import (
	"encoding/hex"

	"github.com/wilfreddenton/merkle"
)

// Anchor defines the Anchor message to send.
type Anchor struct {
	Version        uint8  `json:"version"`
	ChildTangleID  string `json:"childTangleID"`
	LastStampID    string `json:"lastStampID"`
	ChildMessageID string `json:"childMessageID"`
	MerkleRoot     string `json:"merkleRoot"`
}

// AnchorResponse contains the ID of the message sent.
type AnchorResponse struct {
	MessageID  string `json:"messageID,omitempty"`
	MerkleRoot string `json:"merkleRoot,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Proof struct {
	MessageID string `json:"messageID"`
	StampID   string `json:"stampID"`
	Path      []Node `json:"path"`
}

type Verify struct {
	Valid bool   `json:"valid,omitempty"`
	Error string `json:"error,omitempty"`
}

type Node struct {
	Hash     string `json:"id"`
	Position string `json:"position"`
}

func (p *Proof) DecodePath() []*merkle.Node {
	res := make([]*merkle.Node, len(p.Path))
	for i, v := range p.Path {
		hash, _ := hex.DecodeString(v.Hash)
		res[i] = &merkle.Node{
			Hash:     hash,
			Position: v.Position,
		}
	}
	return res
}

func EncodePath(p []*merkle.Node) []Node {
	res := make([]Node, len(p))
	for i, v := range p {
		hash := hex.EncodeToString(v.Hash)
		res[i] = Node{
			Hash:     hash,
			Position: v.Position,
		}
	}
	return res
}
