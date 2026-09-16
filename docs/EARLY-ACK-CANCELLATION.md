# Cancellation before admission

The installed r10 combined automation run reproduced a stranded `starting`
operation: stop-background arrived after begin, but before ack. Ack rejected
the cancellation; the client exited without ever acquiring permission to work,
leaving the global fence behind. The operation was recovered through WebUI.
This reproduction does not establish the original user's incident root cause.

`early-ack-baseline-20260916` reproduces the same lifecycle locally (one failing
test, two ownership controls passing). Ack now finalizes only its exact,
authenticated, not-yet-acknowledged generation as aborted/CANCELLED and retires
its fence. It first verifies owner identity, children absence and settled
publication. It sends no signals and never admits cancelled work. A live job
already admitted by ack retains its fence; a foreign owner cannot retire it.

Validation:

- `early-ack-final-linux-20260916`: 87 tests pass across cancellation,
  coordinator, response loss, boot recovery and recovery flow. These include
  synthetic proc identities and actual Linux owners; they are not router tests.
- `physical-early-ack-20260916-03`: four checks pass with actual ARM owners in
  private prefixes, including a repeated ack after terminal completion.
  Installed Xray identity and business files are unchanged. Prefixes archived
  and retired. The first harness incorrectly used PID 1 as a capturable foreign
  owner; the second had Windows line endings. Both failed harness attempts are
  retained separately and are not counted as product failures.
- Local source/syntax checks pass. Full installed combined acceptance must be
  repeated on the candidate containing this fix.
