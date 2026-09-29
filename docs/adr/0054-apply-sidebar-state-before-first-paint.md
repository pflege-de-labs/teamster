# 0054. Apply the sidebar state before first paint

* Status: Accepted
* Date: 2026-09-29

## Context

[ADR 0042](0042-sidebar-navigation-and-user-menu.md) rendered the burger `hidden` and let
`web/nav.js`, loaded at the end of the body, reveal it and fold the sidebar if the user had folded
it before. Every navigation is a full page load, so every page painted once without the burger and
with the sidebar open, then moved: the logo jumped right when the burger appeared, and the content
jumped left when a folded sidebar was hidden. Pages of different length also toggled the scrollbar,
which shifted the right-aligned user menu.

The no-JavaScript guarantee from ADR 0042 still applies: without a script the sidebar is open and
there is no burger that would do nothing.

## Decision

We will decide the navigation's state in a small inline script in `<head>`, before the body is
parsed. It adds `js` to `<html>` and, if `localStorage` says so, `sidebar-closed`. The burger is
rendered without `hidden` and shown by the `[.js_&]:block` variant; the sidebar is hidden by
`[.sidebar-closed_&]:hidden`. `nav.js` toggles the class on `<html>` instead of the sidebar's
`hidden` attribute. `<html>` reserves the scrollbar gutter (`scrollbar-gutter: stable`).

The sidebar also shows the build version at its bottom, stamped by `-ldflags` as before and handed
to the server through `config.Config.Version`, which is neither a flag nor a file key.

Alternatives considered:

* Loading `nav.js` in `<head>` without `defer`. It needs the burger and the sidebar, which do not
  exist yet at that point.
* Rendering the state on the server from a cookie. It adds a cookie and a request round trip for a
  purely presentational choice.
* Reserving the burger's space with `invisible`. It fixes the logo but still shows an open sidebar
  that then folds.

## Consequences

Pages render in their final layout on first paint. The head carries one inline script, so a future
Content-Security-Policy must allow it by hash or nonce. Signed-in users can see which build they are
using; the login page does not show it.
