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
