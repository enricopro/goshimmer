package anchor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/blake2b"
)

func TestPayload(t *testing.T) {
	h, err := blake2b.New256(nil)
	require.NoError(t, err)
	h.Write([]byte("Mainnet"))
	childTangleID := h.Sum(nil)
	h.Write(childTangleID)
	lastStampID := h.Sum(nil)
	h.Write(lastStampID)
	childMessageID := h.Sum(nil)
	h.Write(childMessageID)
	merkleRoot := h.Sum(nil)
	a := NewPayload(1, childTangleID, lastStampID, childMessageID, merkleRoot)
	fmt.Println(a)
	abytes := a.Bytes()
	b, _, err := FromBytes(abytes)
	require.NoError(t, err)
	assert.Equal(t, a, b)
}
