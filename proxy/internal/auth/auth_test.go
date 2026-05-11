package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashKey(t *testing.T) {
	secret := "test-secret-32-bytes-long-padded-x"

	h1 := HashKey("sk-or-abc", secret)
	h2 := HashKey("sk-or-abc", secret)
	assert.Equal(t, h1, h2, "same input must hash to same output")

	h3 := HashKey("sk-or-xyz", secret)
	assert.NotEqual(t, h1, h3, "different inputs must hash differently")

	assert.Len(t, h1, 64, "HMAC-SHA256 hex is 64 chars")
}
