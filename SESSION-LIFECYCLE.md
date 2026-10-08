# Session lifecycle and admission contract

This change follows the independent review of lifecycle commit `d949e674`.
It does not incorporate PR5's output sanitization or executor changes.

## Nonblocking admission

`Manager.Submit` returns an admission error and delivers **exactly one**
`Result` callback, including rejection. Rejection is immediate and never
waits for FIFO space. Callbacks execute outside session, owner, manager and
budget locks. As with the original callback API, callers must not block
callbacks on unrelated external work; nonblocking admission does not make
arbitrary user callbacks nonblocking.

`Execute`, `ExecuteOwned` and `Session.Execute` retain the integer callback
for existing statement-style callers and now also return the admission
error. Callers must not emit a second completion because the returned error
is non-nil. Function values/interfaces that require the old void-returning
signature need an adapter. `Submit` gives the callback the rejection reason
without a return-value/callback timing race.

| Result | Meaning |
| --- | --- |
| ordinary executor exit code | admitted and executed |
| `-1` | start error or existing executor failure/kill convention |
| `-2` (`ExitCancelled`) | owner already ended, or admitted job cancelled before execution |
| `-3` (`ExitRejected`) + `Result.Err` | never admitted: limit or closing-session rejection |

The TCP completion remains `id`, `done:true`, `exit_code`, `sessions`.
A rejected request additionally has an explicit `error` string and no
output/process. It produces no second error/completion message.

### Defaults

| Scope | Job count | String bytes |
| --- | ---: | ---: |
| single command | — | 256 KiB command text |
| session | 65 | 2 MiB |
| owner across all names | 128 | 4 MiB |
| manager across all owners/names | 512 | 16 MiB |

Limits count **active plus queued** jobs, not just FIFO entries. Accounted
bytes include command, shell path, session name and agent. Name <=128 bytes,
agent <=256 bytes, shell path <=4 KiB. Retained command/name/agent/shell
strings are cloned so a small substring cannot retain a huge caller buffer.
There are also at most **128 named sessions/workers**, including idle and
closing sessions. New names therefore cannot trivially bypass limits.
Ownerless API calls share the manager's nil-owner budget. A new connection
gets a new internal owner, but it cannot bypass the manager-wide budget.

`NewManagerWithLimits` permits a deliberately configured positive limit
policy. Zero/negative limits are invalid, never an unlimited escape hatch.
`Manager.Limits()` returns an immutable copy. `GetOrCreateChecked` exposes
name/worker/closing rejection errors; legacy `GetOrCreate` returns nil on
those rejections. Client protocol cannot change limits or choose an owner.

Credits are retained during process startup/execution/output wait, then
released before the completion callback. Payload references are dropped
before credits are returned. Queue cancellation, drain, start failure,
owner end, shutdown and rejection all release their appropriate budget and
owner registrations. Budget maps remove entries when the job count is zero.
Named-worker capacity is released only at shutdown completion. Idle sessions
remain discoverable and counted until explicit Close; no automatic eviction
of an idle named session was added. **128 completed idle names can reject
further new names indefinitely until an operator closes an old name.**
Existing-name work still runs. Start failure or short-lived owner completion
releases job/byte/owner credits but intentionally keeps the discoverable idle
session slot. Operators can list sessions and send `{"close":true,"session":"old-name"}`;
after that entry has disappeared, the slot is reusable. Tests fill all 128
default slots with completed short-lived owners, verify explicit rejection,
verify existing-name work, and recover capacity through Close/Done. Separate
start-failure and real-TCP operator-Close tests cover the same policy. This
availability trade-off is explicit, not an automatic retention fix.

The previous block-on-full 64-entry channel is **not** restored. TCP can
continue to list/drain/read EOF while full admission is rejected. Socket
writes and user callbacks retain their existing backpressure behavior;
these limits do not claim to bound kernel buffers, child-process heaps,
output size, client count or arbitrary user callback allocations.

## Reentrant shutdown: request vs completion

`Session.Close`/`Manager.Close` request shutdown and return without joining
the worker or invoking queued callbacks on the caller's stack. They can be
called directly inside OutputCallback or CompletionCallback.

- Close marks the actual session closed, increments its generation, group-
  kills its active executor under its per-session lock, and wakes the worker.
- The original worker still waits for its executor and delivers every queued
  cancellation/completion. Existing executor waits/watchdog are retained.
- Until that worker's handoff, the actual object remains registered as
  `closing`. Same-name submissions get `-3`/`ErrSessionClosing`; no second
  process/worker can execute under that name.
- The worker releases the name and closes `Session.Done()` atomically under
  the manager registry lock. Only afterwards can GetOrCreate create a new
  execution context for that name.
- Different names are independent. No manager lock is held during process
  start, group kill, executor wait or callback execution.

`Manager.Close` returns true for the first accepted request; false for a
missing/already-closing session. TCP close responses add `closing:true`
when accepted: **success acknowledges the request, not quiescence**. TCP
callers can observe the closing entry disappear from `list` before reusing
its name, or handle/retry an explicit closing rejection. In-process callers
capture `s := manager.Get(name)`, request Close, and await `s.Done()` outside
that session's callbacks. They should select Done with their own timeout or
context cancellation rather than wait forever.

**Never await a session's Done inside its own output/completion callback.**
The callback must return for worker quiescence; Close itself is reentrant,
not an invitation to reintroduce a manual self-join.

Done reflects worker/process-wait/completion-callback quiescence. Output
ordering follows the executor's existing Wait contract; the baseline's
known PTY/pipe output race is unchanged. With PR5's OutputDone-waiting
executor, the reserved name is retained through that wait too. No sleeps or
watchdog abandonment are used to hide the callback cycle.

## Tests and provenance

The original reviewer fixtures are preserved byte-for-byte in
`.followup-evidence/reviewer-{session,server}-original.go.txt` and in the
untouched independent-review directory `misterclaw-lifecycle-independent-review-20261008.Rn0saYDf`.
Integrated `review_boundary_test.go` files retain the exact finite
`head -c 262144 /dev/zero` output-callback fixture and positive reentrancy
probes. Resource tests change the expectation from 256 accepted jobs to a
bounded queue, and log signed GC heap deltas to avoid unsigned underflow.
Original 256-KiB-plus-prefix payloads now exceed the single-command cap;
separate TCP tests use exact 256 KiB payloads **within** that cap to verify
aggregate byte admission, not merely oversize rejection.

Additional tests cover each count/byte scope, worker-name cap, validation,
100 concurrent submissions, exact once-only rejection completion, cancellation
and credit reuse, foreign owner isolation, direct output callback Close,
held output callback plus same-name rejection/parallel different-name work,
completion callback Close, and Close/recreate after Done.
