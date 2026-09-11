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
// repeated here.

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
// hidden from them, and refused if they post it anyway.
permit (
  principal in Role::"viewer",
  action == Action::"view",
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
