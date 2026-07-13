# Local-tool #118 proof contract

Automated tests cover the finite loader, exact typed lowering, dynamic provider
evidence before `started`, managed queue observation, direct execution bounds,
empty environment, literal metacharacters, exact contract/deployment/digest,
prepare-to-launch tamper, recovery binding mismatch, malformed structured
output, and a data-only unrelated dummy executable.

The real-host Herdr gate is intentionally separate from unit fixtures. On a
profile where envctl PR #28 has emitted
`envctl.verified-executable-bindings.v1`, use the Herdr rows from
`examples/hq.local-tools.jsonl`, set the profile's absolute
`executable_bindings_path`, and submit the semantic instruction:

```json
{"kind":"accepted.instruction","queue":"instruction.jsonl","instruction":{"id":"replace-with-fresh-id","version":"instruction.v1","op":"run","target":"local-tool","payload":{"tool_id":"herdr","tool_version":"1","action_id":"version","input":{}},"created_at":"replace-with-current-utc"}}
```

Run the already-installed managed worker (`hq-worker serve --profile <name>`),
not a per-submit one-shot worker. Passing requires all of the following:

1. envctl registry resolves `local-tool.herdr` to the immutable managed release
   path, not a bare name or `current` shim;
2. a fake `herdr` placed first on PATH is never invoked;
3. canonical evidence contains `accepted`, then `started` with provider ID
   `local-tool.herdr`, expected contract, deployment, and digest;
4. stdout and the terminal `completed` result contain the real Herdr version;
5. removing or changing the binding, deployment, contract, or bytes produces a
   typed non-green result and starts zero unverified processes.

Do not interpret this safe-action gate as complete Herdr action parity or a
Herdr/Vim run-view proof. Those are explicitly outside hq #118.
