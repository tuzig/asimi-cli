package main

import (
	"testing"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
)

func TestRenderSealChain_AllSealsGranted(t *testing.T) {
	seals := []storage.Seal{
		{MinisterID: "judge", SealID: "j1"},
		{MinisterID: "sage", SealID: "s2"},
		{MinisterID: "ruler", SealID: "r3"},
	}
	output := renderSealChain(seals, 80)
	assert.Contains(t, output, "Judge")
	assert.Contains(t, output, "Sage")
	assert.Contains(t, output, "Ruler")
	// All sealed → all show ✓
	assert.Contains(t, output, "✓")
	assert.NotContains(t, output, "○")
}

func TestRenderSealChain_NoSealsGranted(t *testing.T) {
	output := renderSealChain(nil, 80)
	// None sealed → all show ○
	assert.Contains(t, output, "○")
	assert.Contains(t, output, "Judge")
	assert.Contains(t, output, "Sage")
	assert.Contains(t, output, "Ruler")
}

func TestRenderSealChain_PartialSeals(t *testing.T) {
	seals := []storage.Seal{
		{MinisterID: "judge", SealID: "j1"},
	}
	output := renderSealChain(seals, 80)
	// Judge sealed, Sage and Ruler not
	assert.Contains(t, output, "✓")
	assert.Contains(t, output, "○")
	assert.Contains(t, output, "Judge")
	assert.Contains(t, output, "Sage")
	assert.Contains(t, output, "Ruler")
}
