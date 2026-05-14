package repo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRefreshDiff_NilReceiver(t *testing.T) {
	// RefreshDiff must not panic on nil receiver
	var r *RepoInfo
	require.NotPanics(t, func() { r.RefreshDiff() })
}

func TestRefreshDiff_NilRepo(t *testing.T) {
	// RefreshDiff must not panic when repo field is nil
	r := &RepoInfo{}
	require.NotPanics(t, func() { r.RefreshDiff() })
}
