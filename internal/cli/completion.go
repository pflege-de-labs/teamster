package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/miekg/king"
)

// CompletionCmd writes a completion script for one shell to stdout.
//
// The script is generated from kong's own model rather than kept as a file in
// the repository, so a command or a flag added anywhere in the tree is
// completable without anybody remembering to update a list. Generating it is
// [github.com/miekg/king]'s job: three shells is three dialects to get subtly
// wrong, and the interesting parts — positional arguments, enum values, when to
// fall back to filenames — are exactly the parts that are easy to get nearly
// right.
type CompletionCmd struct {
	Shell string `arg:"" enum:"bash,zsh,fish" help:"Shell to generate a completion script for."`
}

func (c *CompletionCmd) Run(kctx *kong.Context) error {
	var completer king.Completer
	switch c.Shell {
	case "bash":
		completer = &king.Bash{}
	case "zsh":
		completer = &king.Zsh{}
	case "fish":
		completer = &king.Fish{}
	default:
		// kong's enum tag rejects anything else long before Run is reached.
		// This is here so that editing that tag cannot silently produce an
		// empty script instead of an error.
		return fmt.Errorf("no completion for %q, want bash, zsh or fish", c.Shell)
	}

	completer.Completion(kctx.Model.Node, "")
	out := completer.Out()
	if c.Shell == "fish" {
		out = nestFishSubcommands(out, kctx.Model.Node)
	}

	_, err := kctx.Stdout.Write(out)
	return err
}

// nestFishSubcommands repairs the one thing king's fish output gets wrong for a
// command tree deeper than one level.
//
// It offers every command with __fish_use_subcommand, which is true only while
// no subcommand has been typed yet. For `teamster migrate up` that is wrong in
// both directions at once: `up` is offered at the top level, where it means
// nothing, and it is not offered after `migrate`, where it is the only thing
// that does. Fish then falls back to filenames, which is how you discover it.
//
// The condition a nested command wants is "its parent has been seen, and none
// of its siblings has". The rewrite is driven from the model rather than by
// pattern-matching the script, so it cannot rewrite a line that happens to look
// similar. It is deliberately narrow, and comes out when king grows the same
// fix — reported at https://github.com/miekg/king/issues.
func nestFishSubcommands(script []byte, root *kong.Node) []byte {
	for _, parent := range commandChildren(root) {
		siblings := commandChildren(parent)
		if len(siblings) == 0 {
			continue
		}

		names := make([]string, 0, len(siblings))
		for _, sibling := range siblings {
			names = append(names, sibling.Name)
		}
		condition := fmt.Sprintf("__fish_seen_subcommand_from %s; and not __fish_seen_subcommand_from %s",
			parent.Name, strings.Join(names, " "))

		for _, child := range siblings {
			old := fmt.Sprintf("-n '__fish_use_subcommand' -a %s -d ", child.Name)
			script = bytes.Replace(script, []byte(old),
				[]byte(fmt.Sprintf("-n '%s' -a %s -d ", condition, child.Name)), 1)
		}

		// Anything deeper is the same problem one level down, and this CLI has
		// none today — but a tree that grows one should not quietly regress.
		script = nestFishSubcommands(script, parent)
	}
	return script
}

// commandChildren is the subcommands of a node: the children that are commands
// somebody types, rather than positional arguments or hidden plumbing.
func commandChildren(node *kong.Node) []*kong.Node {
	var out []*kong.Node
	for _, child := range node.Children {
		if child.Type == kong.CommandNode && !child.Hidden {
			out = append(out, child)
		}
	}
	return out
}
