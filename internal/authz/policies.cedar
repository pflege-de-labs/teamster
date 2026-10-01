// Who may do what in the admin UI and the admin API.
//
// Every role the identity provider names for a user arrives here by the same
// name, so a deployment can define a role of its own and add a policy for it
// below. A role no policy mentions grants nothing.
//
// admin, editor and viewer are required: the admin UI asks about them when it
// decides whether to show a control, and auth-default-role is one of them.
// Add roles freely; do not remove these three.
//
// The three nest — an admin is an editor, an editor is a viewer — which is
// expressed as the parents of the Role entities in authz.go rather than
// repeated here. Actions nest too (actions.go): read is part of view, and
// create, update, delete and attach are part of edit, so the roles below cover
// them. share and transfer are part of own alone, which no role is given.

// An admin may do anything, including whatever actions later milestones add.
permit (
  principal in Role::"admin",
  action,
  resource
);

// An editor may read the configuration and change it.
permit (
  principal in Role::"editor",
  action in [Action::"view", Action::"edit"],
  resource
);

// A viewer may look and no more. Everything the UI offers beyond looking is
// hidden from them, and refused if they post it anyway. `in` rather than `==`
// so that read, which is part of view, is covered too.
permit (
  principal in Role::"viewer",
  action in Action::"view",
  resource
);

// Linking a chat binds the caller's own subject, nobody else's, so a viewer
// may do it without being handed edit on anything the link touches. Admin and
// editor inherit it through the Role nesting above.
permit (
  principal in Role::"viewer",
  action == Action::"link",
  resource
);

// Where a role may deliver, and what it may see of the directory.
//
// A role with no grant is unrestricted: an installation nobody has narrowed
// behaves as it did before grants existed. Once a grant names a role, that role
// reaches what its grants name and nothing else. Admins are covered by the
// policy at the top of this file and are never scoped.

permit (
  principal in Role::"editor",
  action == Action::"deliver",
  resource
) when { principal.unrestricted || resource in principal.scopes };

permit (
  principal in Role::"viewer",
  action == Action::"viewChannel",
  resource
) when { principal.unrestricted || resource in principal.scopes };

permit (
  principal in Role::"viewer",
  action == Action::"viewTeam",
  resource
) when { principal.unrestricted || resource in principal.teams };

// A route may deliver to a person's chat only for that person. The resource is
// the recipient's own User entity, so the check is who, not which role; admins
// pass through the policy at the top of this file. Grants on users or groups
// widen this rule later, here rather than in Go.
permit (
  principal,
  action == Action::"deliverToRecipient",
  resource
) when { principal == resource };

// A route that delivers to the people a message names reaches anyone in the
// tenant, so only admins may point one there, through the policy at the top.
// To let a custom role do it too:
//
// permit (
//   principal in Role::"it-messaging",
//   action == Action::"deliverToAddressed",
//   resource
// );

// Who may send to the webhooks with a token of their own (ADR 0076). Editors
// may, admins through the policy at the top; anyone else needs a grant of use.
permit (
  principal in Role::"editor",
  action == Action::"use",
  resource in Webhook::"*"
);
