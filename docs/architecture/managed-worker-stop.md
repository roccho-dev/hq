# Managed worker graceful stop

Issue owner: #98

`hq-worker stop --profile <name>` is the supported cross-platform stop control for an installed managed worker. It removes the former dependency on whether a Windows supervisor can attach to the worker console and deliver `CTRL_C_EVENT`.

```text
fresh exact claim + heartbeat
  -> atomic stop request bound to claim/profile/deployment
  -> current hq-worker serve observes that exact request
  -> existing context cancellation and recovery semantics
  -> lifecycle state=stopped
  -> heartbeat and claim release
  -> worker-written exact stop receipt
  -> stop caller verifies receipt + no replacement claim
```

A missing process or claim is not graceful-stop proof. The caller succeeds only after reading the worker-written receipt for the exact request. A crash after request creation but before acknowledgement remains timeout/non-green.

The control records are local runtime evidence only. They do not become instruction, result, queue, deployment, or remote authority. The path uses no shell, PowerShell, cmd, PATH lookup, PID kill, network listener, message broker, or general command channel.

`envs` remains responsible for installed activation and supervision. An installed-environment proof must start the exact hq artifact, observe ready health, invoke the exact stop command, read the receipt, verify zero claim/heartbeat, restart, and verify ready health again.
