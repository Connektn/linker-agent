
# Story – Linker Agent: Poll-Based Control Commands

## Before You Start
- Follow all rules in `CLAUDE.md`.
- This prompt defines the **agent-side** implementation of a poll-based command system.
- Heartbeats remain fast and unchanged.

---

# Goal
Add support to the agent for:

1. Polling a dedicated cloud endpoint at a configurable interval.
2. Parsing optional commands returned by the poll endpoint; supported commands: `restart`, `switch_mode`, `upgrade`.
3. Executing commands.
4. Reporting results to a designated command-result endpoint.
5. Handling malformed or unknown commands gracefully.

Use CONTROL_SECRET for signing requests.

---

# Architecture Overview (Agent)

## 1. Configuration Additions
Add the following configurable fields:

```yaml
cloud:
  baseUrl: "https://cloud.example.com"
  heartbeatEndpoint: "/api/agents/{agentId}/heartbeats"
  commandPollEndpoint: "/api/agents/{agentId}/commands/poll"
  commandResultEndpointTemplate: "/api/agents/{agentId}/commands/{commandId}/result"
  commandPollIntervalSeconds: 60   # default
```

---

## 2. Poll Loop

Add a goroutine:

```
for {
    sleep(pollInterval)
    pollForCommands()
}
```

`pollForCommands()`:
1. Build request with `agentId`, `organizationId`, timestamp, signature.
2. POST to `commandPollEndpoint`.
3. If empty body → return.
4. Else parse JSON:
   ```
   {
     "commandId": "...",
     "command": "restart",
     "params": { "mode": "graceful" }
   }
   ```
5. Dispatch to command executor.

---

## 3. Command Execution

Add a dispatcher:

```go
type Command struct {
    CommandId string
    Command   string
    Params    map[string]any
}

func (a *Agent) handleCommand(cmd Command) (status, message string) {
    switch cmd.Command {
    case "restart":
        return a.executor.Restart(cmd.Params)
    case "upgrade":
        return a.executor.Upgrade(cmd.Params)
    default:
        return "error", "unknown command"
    }
}
```

Executor responsibilities:
- `Restart`: graceful restart (supervisor signal)
- `Upgrade`: placeholder for future version upgrade
- `SwitchMode`: change privacy mode without restart
- Return `(status, message)` where status = `"success"` | `"error"`

---

## 4. Reporting Command Results

After execution:

1. Construct:

```
{
  "status": "success",   // or "error"
  "message": "Restarted gracefully"
}
```

2. POST to:
   - `commandResultEndpointTemplate`
   - substitute `{agentId}`, `{commandId}`

If the request fails:
- Log error
- Optionally retry once or rely on cloud-side timeout
- Continue normal operation

---

## 5. Error Handling
- Malformed command → `"error"` + log
- Unknown command → `"error: unknown command"`
- Execution panic → catch, log, report `"error"`
- No command persistence needed locally
- Poll interval strictly controls frequency (no tight looping)

---

# Acceptance Criteria

1. Agent polls for commands regularly.
2. Empty poll response results in no action.
3. Valid command results in correct execution.
4. Execution result POSTed to cloud.
5. Agent does not crash on malformed commands.
6. Heartbeat logic remains unaffected.
7. Poll interval is configurable.

---

# Deliverables
- Poll loop implementation
- Command dispatcher & executor
- Result reporting client
- Updated documentation: `/docs/linker-agent-control.md`
