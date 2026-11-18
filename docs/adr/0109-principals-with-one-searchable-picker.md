# 0109. Name principals with one searchable picker

* Status: Accepted
* Date: 2026-11-18

## Context

The webhook and message level forms took a principal type and a free-text id, and the group member
form a type and a datalist. A group typed by its name instead of its id saved a grant that matched
nobody and was listed as if it worked (#327). Admins also could not tell a user's subject from a
display name.

## Decision

We will render every principal field with one server-rendered `<select>` (`principalPicker`) whose
values are `type:id`, and enhance it with `principal-picker.js` into a searchable list. Each form
chooses the kinds it offers. A person who has not signed in is entered in a collapsed "Someone not
listed" block (`other_type`, `other_id`). Handlers read the result with `principalFromForm`.

A client-side combobox library lost: the page works without script, and the server already knows
the options.

## Consequences

* Ids are chosen, not typed; unknown local groups cannot be entered.
* Without script the select is still usable, only unsearchable.
* The options are the first page of users, so a large directory needs the "not listed" block.
* The JSON APIs and revoke buttons keep `principal_type` and `principal_id`.
