# Story 2 – Tenant Agent Management & Heartbeats

## Before you start
Read and follow all rules in `CLAUDE.md`.  
Use the existing Linker Agent heartbeat logic and extend it to tenant-visible status in the Cloud UI.

---

## Goal
Enable tenants to **view and control their on-prem Linker Agents** directly from the Connektn Cloud dashboard.

Agents must send periodic heartbeats containing health, queue, and mode data, and accept signed control commands from the cloud (restart, stop, switch mode, upgrade).

---

## Scope
### Agent-side
- Extend heartbeat payload to include:
  - agent_id, organization_id, version, uptime, mode, queue_depth, dlq_size, retry_count, last_flush_ts.
  - optional resource usage (CPU%, memory%, disk usage, network lag).
- Send heartbeat every 30s (configurable).
- Implement a control command handler:
  - Securely validate cloud signature.
  - Support commands: `restart`, `stop`, `start`, `switch_mode`, `upgrade`.
  - Queue pending command results to next heartbeat.

### Cloud-side
- API endpoints for agent status and control actions:
  - `GET /api/agents` (list + filter by org).
  - `POST /api/agents/{id}/actions` with command + signature.
- Store agent health data in time-series DB (Influx/ClickHouse/Timescale).
- Update tenant dashboard UI:
  - Agent card showing version, mode, uptime, queue depth, DLQ size.
  - Action buttons for control commands.

### Security
- Heartbeat messages signed with agent keypair.
- Cloud commands signed with Connektn master key, verified by agent.
- Replay protection (nonce + timestamp).

---

## Acceptance Criteria
1. Agents send regular heartbeats; dashboard updates live.
2. Tenant sees accurate agent mode, uptime, queue depth, and DLQ.
3. Control actions execute safely and return result codes.
4. Signature verification prevents spoofing or replay.
5. No cloud credentials stored in agent.
6. Heartbeat delay >2 intervals triggers `unhealthy` status.

---

## Test Plan
- Unit: signature validation, replay protection, command parsing.
- Integration: agent ↔ cloud communication over mTLS.
- UI: manual verification of heartbeat updates and action buttons.
- Stress: simulate 1k tenants (1k agents) with 30s heartbeat cadence.

---

## Deliverables
- Agent heartbeat module under `agent/heartbeat/`.
- Cloud API endpoints under `cloud/api/agents/`.
- Dashboard UI component: `AgentStatusCard.vue`.
- Documentation `/docs/tenant-agent-management.md`.

---

**Author:** Tomas Zezula  
**Status:** Backlog
