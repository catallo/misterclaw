# PTY output and completion

PTY commands have a single child reaper and one stored completion result. Every `Wait` observes that same result. Normal completion is published only after the PTY reader has delivered its final output and the synchronous output callback has returned. Linux terminal EIO after the last slave closes is treated as normal EOF.

The PTY master is duplicated with close-on-exec, switched to nonblocking mode, and wrapped in a new pollable `os.File`. Merely setting a deadline on the original blocking file is insufficient. No later code may call `File.Fd` on this master: it can revert the descriptor to blocking mode. Terminal resizing therefore uses a raw control operation without changing file flags.

After the direct child exits, a five-second read-drain deadline bounds terminal I/O when a descendant or another open slave keeps the terminal alive. Expiry returns `ErrPTYDrainTimeout` and a non-success exit result instead of silently claiming complete output. The error reaches the existing session/TCP completion `error` field. This is not an inactivity timeout while the direct child is still running.

`Kill` is a cancellation request, not a join; it can be called from an output callback. Joins and callbacks occur outside executor locks. A reaped child's process group is not signalled through its potentially reused PID. The I/O deadline still bounds a retained terminal after direct-child exit.

Callbacks must not call and await the same executor's `Wait`, session completion, or another operation that requires their own callback to return. An arbitrary synchronous callback cannot be forcibly cancelled: the five-second limit bounds reader I/O, not callback execution time. A slow callback can exhaust the I/O drain grace and produce an explicit incomplete-output result after it returns. The existing session watchdog and its last-resort fallback are unchanged; this fix does not promise an unconditional end-to-end deadline or output-before-completion after that fallback.

The raw `PipeExecutor` implementation, owner/admission limits, idle-name retention policy, setsid boundaries and watchdog durations remain unchanged. Release builds/CI are not deployment to a production MiSTer.
