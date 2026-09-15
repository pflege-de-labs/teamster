# 0024. Scan images with Trivy and report to SecObserve

* Status: Accepted
* Date: 2026-09-15

## Context

The image carries an SBOM and is signed (ADR 0006), which proves what went into it and where it
came from. Neither says whether any of it has a known vulnerability. That question was previously
answered nowhere: not on a push to `main`, not on a release, not anywhere a human would see it
before it shipped.

SecObserve is the vulnerability-management service this organisation already runs findings
through for other repositories, reachable at `https://webhooks.p4e.io/secobserve`. Its own action
templates (`SecObserve/secobserve_actions_templates`) wrap Trivy and post the results there in one
step, which is the pattern `~/github/tranquila/.github/workflows` already uses.

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
  `github.ref_name`, which is `main` for the case that runs.

Release images are not yet in scope — see Consequences.

## Consequences

* A finding blocks nothing today: the step has no failure threshold and the job does not fail on
  its result. SecObserve is where the finding is triaged, not the pipeline. Gating merges or
  releases on severity is a later decision if the noise level in practice calls for it.
* **Release images (`release.yml`, tags `v*`) are not scanned.** They are the artifact that is
  actually deployed, and the gap is real: a vulnerability introduced between the last `main` scan
  and a release would go unreported at the moment it matters most. `release.yml` already builds
  with `sbom: true` and signs by digest, so extending this there is the same shape of change; it
  is deferred pending a decision on whether SecObserve should track releases as their own branch
  (by tag) or fold into `main`.
* Requires the `SO_API_TOKEN` secret, provisioned in the repository already.
