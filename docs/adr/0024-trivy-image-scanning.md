# 0024. Scan images with Trivy and report to SecObserve

* Status: Accepted
* Date: 2026-09-15

## Context

The image carries an SBOM and is signed (ADR 0006), which proves what went into it and where it
came from. Neither says whether any of it has a known vulnerability. That question was previously
answered nowhere: not on a push to `main`, not on a release, not anywhere a human would see it
before it shipped.

SecObserve is the vulnerability-management service this organisation already runs findings
through for other repositories, reachable at `https://webhooks.management.p4e.io/secobserve`.
Its own action templates (`SecObserve/secobserve_actions_templates`) wrap Trivy and post
the results there in one step, which is the pattern `~/github/tranquila/.github/workflows`
already uses.

## Decision

The CI `image` job scans the image it just built with Trivy and uploads the findings to
SecObserve, using `SecObserve/secobserve_actions_templates/actions/SCA/trivy_image`, pinned to a
commit sha as every other action in this repository is.

* The scan targets the digest `docker/build-push-action` just produced
  (`${{ env.IMAGE }}@${{ steps.build.outputs.digest }}`), not a tag, which could be repointed
  between the build and the scan.
* It is **skipped entirely for `pull_request` events**, including same-repository ones that do
  push an image. SecObserve creates a "branch" per `so_branch_name` on first import, and a
  transient PR branch has no reason to leave a permanent-ish trace in a findings tool. This
  repository's CI otherwise only runs on push to `main`, so what remains is exactly the branch
  worth tracking over time.
* `so_product_name` and `so_origin_service` are both `teamster`; `so_branch_name` is
  `github.ref_name`.

`release.yml` scans too, once the image is built, signed and its signature verified. There
`github.ref_name` is the release tag itself (`v1.2.3`), not `main` — each release is tracked in
SecObserve as its own branch, so a finding is attributed to the release it shipped in rather than
blurred into the ongoing `main` history CI already reports. That also means a vulnerability found
in a past release stays visible under that release's branch even after `main` has moved on and
been rescanned clean.

## Consequences

* A finding blocks nothing today: the step has no failure threshold and the job does not fail on
  its result, in CI or in a release. SecObserve is where the finding is triaged, not the pipeline.
  Gating merges or releases on severity is a later decision if the noise level in practice calls
  for it.
* SecObserve accumulates one branch per release tag, indefinitely — there is no pruning. That is
  the intended shape (a released version's findings should not disappear because a newer one
  shipped), but it means the branch list in SecObserve grows with every release, unlike `main`
  which stays a single branch reused across every CI run.
* Requires the `SO_API_TOKEN` secret, provisioned in the repository already.
