package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCeilDiv(t *testing.T) {
	assert.Equal(t, int64(1), CeilDiv(1, 1))
	assert.Equal(t, int64(1), CeilDiv(1, 2))
	assert.Equal(t, int64(2), CeilDiv(3, 2))
	assert.Equal(t, int64(0), CeilDiv(0, 1))
	assert.Equal(t, int64(1), CeilDiv(999_999, 1_000_000))
	assert.Equal(t, int64(1), CeilDiv(1_000_000, 1_000_000))
	assert.Equal(t, int64(2), CeilDiv(1_000_001, 1_000_000))
}

func TestCalculateCost(t *testing.T) {
	cost := CalculateCost(1000, 500, 250, 1000, 18)
	assert.Equal(t, int64(1), cost.InputCents)
	assert.Equal(t, int64(1), cost.OutputCents)
	assert.Equal(t, int64(2), cost.ProviderCents)
	assert.Equal(t, int64(1), cost.MarginCents)
	assert.Equal(t, int64(3), cost.TotalCents)
}
