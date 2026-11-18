package views

import "slices"

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
