package views

import (
	"context"
	"slices"

	"github.com/pflege-de-labs/teamster/internal/i18n"
)

// ActionRef is a grant in five characters, r w d a s, or "owner" when it holds everything.
func ActionRef(actions []string) string {
	if slices.Contains(actions, "own") {
		return "owner"
	}
	ref := []byte("-----")
	for i, a := range []string{"read", "update", "delete", "attach", "share"} {
		if slices.Contains(actions, a) {
			ref[i] = "rwdas"[i]
		}
	}
	return string(ref)
}

// actionRef is ActionRef with the owner in the reader's language.
func actionRef(ctx context.Context, actions []string) string {
	if slices.Contains(actions, "own") {
		return i18n.T(ctx, "sharing.owner")
	}
	return ActionRef(actions)
}
