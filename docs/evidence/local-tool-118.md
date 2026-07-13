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

Use an isolated profile. Start exactly one managed `hq-worker serve --profile
<name>` instance before submission and keep that same instance ready throughout
the proof. Do not start a per-submit one-shot worker. Worker installation and
normal supervision remain with the existing envs owner and are not proved here.
Passing requires all of the following:

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

## WSLC data-only consumer

`examples/hq.local-tools.jsonl` fixes the repository-owned WSLC meaning:
tool `wslc` version `2.9.3.0`, binding reference `local-tool.wslc`, action
`version`, and one literal argument `version`. The matching `wslc.version`
command lowers to semantic local-tool identity and empty typed input only.
Neither the world row nor the instruction contains an executable path, digest,
deployment identity, or envs registry layout.

After envs #31 emits and verifies the selected executable binding, prove the
native consumer with an isolated profile:

1. select the exact WSLC rows as world data and set the profile's absolute
   `executable_bindings_path` to the envs-produced registry;
2. poison `PATH` with a same-name fake executable, then start exactly one
   managed `hq-worker serve --profile <name>` instance before submission and
   keep that same instance ready throughout the proof;
3. do not start any per-submit one-shot worker; installation and normal
   supervision remain with the existing envs owner and are outside this proof;
4. submit exactly one `@wslc.version`, which must append this semantic payload:

   ```json
   {"tool_id":"wslc","tool_version":"2.9.3.0","action_id":"version","input":{}}
   ```

5. require canonical `result.v1` evidence in the order `accepted`, `started`,
   `stdout`, `completed`; the `started` provider must identify
   `local-tool.wslc`, contract `1`, and the registry-supplied deployment and
   digest, while stdout/final report WSLC `2.9.3.0`;
6. verify the poisoned executable was never invoked and that changing the
   binding, contract, deployment, or bytes produces typed non-green evidence
   before any unverified process starts.

The executable location and integrity remain envs/profile data. They must not
be copied into the public world example or the queued instruction.
