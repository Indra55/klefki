# M0 status

Spec review and verification are complete. The specification and vectors are
committed as part of reconstructed milestone history.

The user approved the crypto spec, threat model and proposed CLI container
layout, restored npm access and initialized Git. On rechecking, the real
TypeScript/libsodium verifier passed and the npm lockfile was present. Go fixture
generation, reproduction tests, recovery checks and vet also passed. The earlier
npm `EAI_AGAIN` blocker is resolved.

During the original implementation session, Git metadata was read-only and
commits could not be created. The artifacts are now committed; their assigned
commit dates do not replace the recorded approval or verification dates.
The user explicitly authorized continuing M1; it is tracked separately.

Delivered artifacts:

- Full `PLAN.md`, reviewed threat model and v1 crypto spec.
- Three KDF vectors, seven complete encrypted records, six stream boundary
  recipes, recovery encoding and twenty-five shared rejection cases.
- Go generator/verifier, independent TypeScript verifier, pinned dependencies and
  npm lockfile. No fixture secrets are real account credentials or real documents.

These checks establish record interoperability under Go and Node, not browser
runtime support, cryptographic audit, server security or device performance.
The M1 container layouts were added under the user's separate approval; existing
M0 encrypted-record bytes and vectors were not changed.
