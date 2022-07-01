package jsonmodels

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
