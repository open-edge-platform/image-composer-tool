# Running builds on a farm: the remote build protocol

**Who this is for.**

- an **operator** who wants `image-composer-tool serve` to send builds to a
  build farm instead of running them on its own host — start at [Running
  `serve` against a build service](#running-serve-against-a-build-service);
- a **developer** writing the **build service** that `serve` talks to, or
  reviewing ICT's client for it — read on from [The protocol in one
  table](#the-protocol-in-one-table).

**In one sentence.** The remote build protocol is a small HTTP + Server-Sent
Events contract: `serve` submits a self-contained template, follows a resumable
event stream of log lines, phases and worker detail, can cancel, and can read a
finished build back. Any program in any language that implements it is a build
service; the Web UI, the API and build history behave the same whichever
backend runs the build.

**Normative document:** [`api/v1/remote-build-protocol.yaml`](../../../api/v1/remote-build-protocol.yaml)
(OpenAPI 3.0). This page explains it; where they differ, the YAML wins.

```mermaid
flowchart LR
    B[Browser<br/>Web UI] -->|REST + SSE| S["serve --executor remote<br/>(API, history, SSE: unchanged)"]
    S -->|remote build protocol<br/>HTTP + SSE, bearer token| BS["build service<br/>(yours: any language)"]
    BS --> W1[worker]
    BS --> W2[worker]
    W1 -->|image-composer-tool build| A[(published artifacts)]
    W2 -->|image-composer-tool build| A
    A -. url, sha256 .-> S
```

The build service is a **thin bridge**: it starts `image-composer-tool build`
somewhere, relays what happens, and publishes the outputs. Template semantics
stay with ICT on the worker; the service never interprets the template.

## Running `serve` against a build service

```bash
image-composer-tool serve \
  --executor remote \
  --executor-url http://ict-farm-bridge:9010 \
  --templates-dir image-templates
```

| Flag (env) | Default | Meaning |
|---|---|---|
| `--executor` (`ICT_EXECUTOR`) | `local` | `local` runs builds on this host; `remote` sends them to a build service |
| `--executor-url` (`ICT_EXECUTOR_URL`) | | The build service's base URL; required with `remote` |
| `--executor-token` (`ICT_EXECUTOR_TOKEN`) | | Sent as `Authorization: Bearer` on every request when set |
| `--max-concurrent` (`ICT_MAX_CONCURRENT`) | the service's | Builds the UI may start at once; may only lower the service's number. Must stay 1 with `local` |

What happens at start, and why it matters to you:

- `serve` calls `GET /v1/info` and **refuses** a service that speaks another
  protocol version, so a mismatched deployment fails loudly at start rather
  than quietly mid-build.
- The service's `maxConcurrent` becomes the number of builds the Web UI lets
  users start at once, and its `phases` become the Web UI's stepper.
- Builds a previous `serve` left running are **picked up again** (see
  [Recovering a build after `serve` restarts](#recovering-a-build-after-serve-restarts)),
  so a restart of the UI server never loses a farm build.
- Shutdown leaves farm builds running; they are not this host's processes.
- `serve` itself needs no privileges in this mode: it runs no build.

## The protocol in one table

| Call | Purpose |
|---|---|
| `GET /v1/info` | `{name, protocol: "1", maxConcurrent, phases[]}` — capabilities ICT sizes itself from |
| `POST /v1/builds` | submit `{id, template, files[], labels}`; idempotent on `id`; `202 {id, status, …}` |
| `GET /v1/builds/{id}` | `{id, status, phase, meta, error, artifacts}` — the build's current state |
| `GET /v1/builds/{id}/events?from=N` | SSE: `log {n, line}`, `phase {id, label}`, `meta {key, value}`, `done {status, error, artifacts}` |
| `POST /v1/builds/{id}/cancel` | `202`; the stream then ends with `done {status: cancelled}` |

Statuses: `queued`, `running`, `cancelling`, `succeeded`, `failed`, `cancelled`.
Artifacts: `{name, type ∈ image|sbom|log|other, sizeBytes, sha256, url}`; the Web
UI links to `url`.

## Resources and events

```mermaid
classDiagram
    direction LR

    class Info {
        +name string
        +protocol string  always 1
        +maxConcurrent int
        +phases List~Phase~
    }

    class Phase {
        +id string
        +label string
    }

    class BuildSubmission {
        +id string  ICT's build id
        +template string  self-contained YAML
        +files List~File~
        +labels map of string to string
    }

    class File {
        +path string  relative to the templates directory
        +contentBase64 string
        +mode string  optional, e.g. 0755
    }

    class BuildState {
        +id string
        +status Status
        +phase Phase
        +meta map of string to string
        +error string
        +artifacts List~Artifact~
    }

    class Status {
        <<enumeration>>
        queued
        running
        cancelling
        succeeded
        failed
        cancelled
    }

    class Artifact {
        +name string
        +type string  image, sbom, log or other
        +sizeBytes int64
        +sha256 string
        +url string
    }

    class LogEvent {
        <<SSE event log>>
        +n int64  0-based line index
        +line string
    }

    class PhaseEvent {
        <<SSE event phase>>
        +id string
        +label string
    }

    class MetaEvent {
        <<SSE event meta>>
        +key string
        +value string
    }

    class DoneEvent {
        <<SSE event done>>
        +status Status  terminal only
        +error string
        +artifacts List~Artifact~
    }

    Info --> Phase : phases
    BuildSubmission --> File : files
    BuildState --> Status
    BuildState --> Phase : phase
    BuildState --> Artifact : artifacts
    DoneEvent --> Status
    DoneEvent --> Artifact : artifacts
```

Every resource is keyed by the **build id ICT chose**. That is what lets a
restarted `serve` ask about a build it started earlier, and what makes
`POST /v1/builds` idempotent: a second submission with the same `id` returns
the existing build and starts nothing.

## Build lifecycle

```mermaid
stateDiagram-v2
    [*] --> queued : POST /v1/builds
    queued --> running : a worker picks it up
    queued --> cancelled : cancel before start
    running --> succeeded : build finished
    running --> failed : build finished with an error
    running --> cancelling : POST /v1/builds/{id}/cancel
    cancelling --> cancelled : worker stopped
    cancelling --> succeeded : it finished first
    cancelling --> failed : it finished first
    succeeded --> [*]
    failed --> [*]
    cancelled --> [*]
```

`succeeded`, `failed` and `cancelled` are **terminal**: the event stream ends
with a `done` event carrying that status, and `GET /v1/builds/{id}` keeps
answering it for as long as the service remembers the build. A cancel that
arrives after the build finished is answered `409`; the client treats that as
"already done" and reads the real outcome.

## Sequence: a build from submission to done

```mermaid
sequenceDiagram
    autonumber
    participant ICT as serve (remote executor)
    participant BS as build service
    participant W as worker

    Note over ICT,BS: once, at serve start
    ICT->>BS: GET /v1/info
    BS-->>ICT: 200 {name, protocol: "1", maxConcurrent, phases[]}
    Note over ICT: protocol ≠ "1" → refuse to start

    Note over ICT,W: per build
    ICT->>BS: POST /v1/builds {id, template, files[], labels}
    BS-->>ICT: 202 {id, status: queued}
    ICT->>BS: GET /v1/builds/{id}/events?from=0
    BS-->>ICT: event: phase {id, label} · event: meta {key, value}  (current state, resent on every connect)
    BS->>W: start image-composer-tool build
    loop while the build runs
        W-->>BS: console output
        BS-->>ICT: event: log {n: 0, line} … event: log {n: k, line}
        BS-->>ICT: event: phase {id, label}  (when it changes)
        BS-->>ICT: : heartbeat  (keeps idle connections alive)
    end
    W-->>BS: exit · outputs published
    BS-->>ICT: event: done {status: succeeded, artifacts: [{name, type, sizeBytes, sha256, url}]}
    Note over BS,ICT: the stream ends after done
```

## Sequence: resuming a broken stream

The log is numbered so that a client that reconnects **neither loses nor
repeats** a line. `from` is the index of the first line the client still
needs; `phase` and `meta` are resent in full on every connect, since they
describe the current state rather than a position in the log.

```mermaid
sequenceDiagram
    autonumber
    participant ICT as serve (remote executor)
    participant BS as build service

    ICT->>BS: GET /v1/builds/{id}/events?from=0
    BS-->>ICT: log n=0 … log n=41
    Note over ICT,BS: connection drops (proxy idle timeout, pod restart, network)
    ICT->>BS: GET /v1/builds/{id}  (did it finish meanwhile?)
    BS-->>ICT: 200 {status: running, phase, meta}
    ICT->>BS: GET /v1/builds/{id}/events?from=42
    BS-->>ICT: phase, meta (current) · log n=42 … · done {…}
```

## Sequence: cancelling

```mermaid
sequenceDiagram
    autonumber
    actor U as User (Web UI)
    participant ICT as serve (remote executor)
    participant BS as build service
    participant W as worker

    U->>ICT: Cancel build
    ICT->>BS: POST /v1/builds/{id}/cancel
    BS-->>ICT: 202 Cancellation requested  (409 if it already finished)
    BS->>W: stop the build
    Note over ICT,BS: ICT keeps following the event stream for the real outcome
    W-->>BS: stopped
    BS-->>ICT: event: done {status: cancelled}
    ICT-->>U: build shows cancelled
```

## Sequence: a finished build read back

A client that lost track of a build — typically `serve` after a restart — reads
its state and, for the log, replays the stream, which a finished build ends
immediately with `done`.

```mermaid
sequenceDiagram
    autonumber
    participant ICT as serve (remote executor)
    participant BS as build service

    ICT->>BS: GET /v1/builds/{id}
    alt finished
        BS-->>ICT: 200 {status: succeeded | failed | cancelled, error, artifacts, meta}
        ICT->>BS: GET /v1/builds/{id}/events?from=0
        BS-->>ICT: log n=0 … log n=last · done {…}
    else still running
        BS-->>ICT: 200 {status: queued | running | cancelling}
        Note over ICT: re-attach: follow the stream as for a new build
    else unknown
        BS-->>ICT: 404
        Note over ICT: the build is recorded as failed with an explanation
    end
```

## What the service receives

`POST /v1/builds` carries ICT's build id, the **complete** template (the
Advanced tab's delta and every `extends` already folded in; the worker's ICT
applies only its OS defaults), and the auxiliary files the template references
relative to the templates directory — `additionalFiles[].local` and
`initramfs.template` — so the worker does not need to carry them in its image.
`labels` names the template's image and whatever the deployment configured
(an environment name, say).

## Rules a service must follow

- Key everything by ICT's `id`, and make `POST /v1/builds` idempotent on it.
- Number log lines from 0 and honour `from`, so a reconnecting client neither
  loses nor repeats lines. Resend `phase` and `meta` on every connect.
- End a finished build's stream with `done` immediately on connect.
- Answer `429` when you have no free capacity; ICT does not retry, it reports
  the build as failed to start. Report an honest `maxConcurrent` in `/v1/info`
  and ICT will not ask for more than that.
- Send a heartbeat line (`:`) on an idle stream so proxies do not close it.
- Within protocol `1`, add fields but never remove or rename them.

## How ICT's client behaves

The client is `internal/api/executor/remote` (`remote.Executor`). It implements
the `Executor`, `PhaseReporter` and `Recoverer` interfaces of the [executor
seam](./web-ui-build-executors.md); this is how those three map onto the
protocol:

| Seam method | Protocol calls |
|---|---|
| `New` (at `serve` start) | `GET /v1/info`; refuses another protocol version |
| `Run` | `POST /v1/builds`, then `GET …/events?from=N` until `done` |
| `Cancel` | `POST …/cancel` (`202` and `409` both count as success) |
| `Recover` | `GET /v1/builds/{id}`; for a finished build also `GET …/events?from=0` for the log |
| `Phases` / `MaxConcurrent` | from the `Info` read at start |

Its loop around the event stream:

```mermaid
sequenceDiagram
    autonumber
    participant RUN as Run / follow
    participant RS as readStream
    participant BS as build service

    RUN->>RS: readStream(from)
    RS->>BS: GET …/events?from=N
    alt stream opened
        BS-->>RS: events…
        RS->>RS: forward log → AppendLog, phase → SetPhase, meta → PutMeta
        RS-->>RUN: done event, or next index when the stream broke
        Note over RUN: a stream that opened resets the back-off
    else could not open
        RS-->>RUN: error, same index
    end
    alt done received
        RUN-->>RUN: return its status, artifacts, error
    else stream broke
        RUN->>BS: GET /v1/builds/{id}
        alt finished meanwhile
            RUN-->>RUN: return that outcome
        else
            RUN->>RUN: back off 1 s, 2 s, 4 s … 10 s · give up after ReconnectMax (10 min)
            RUN->>RS: readStream(next)
        end
    end
```

Two bounded waits protect the user from a service that stops answering:

- **Cancel grace** (`CancelGrace`, 2 min). After `Cancel`, `Run` keeps following
  the stream so the build's real outcome is recorded. If the service has not
  sent `done` by then, the build is reported `cancelled` with what is known.
- **Reconnect budget** (`ReconnectMax`, 10 min). A stream that keeps breaking is
  retried with back-off (1 s doubling to 10 s); a stream that opens resets the
  budget. Once exhausted, `Run` fails the build with "lost the build's event
  stream".

Every call but the event stream goes through `network.NewSecureHTTPClient()`,
whose header timeout bounds it; the stream is read for as long as it flows.

## Recovering a build after `serve` restarts

If `serve` stops while farm builds are running, their records on disk still say
`running`. At the next start, before it serves requests, `serve` asks the build
service about each of them: a build that finished is recorded with its real
outcome and its log replayed; a build that is still running is re-attached and
followed to completion, exactly as if `serve` had never stopped; a build the
service no longer knows is recorded as failed with an explanation. The sequence
is [a finished build read back](#sequence-a-finished-build-read-back) above.

## Writing a build service

The client's tests run against an in-memory reference service
(`internal/api/executor/remote/fake_service_test.go`) that is a faithful,
minimal implementation to copy from, and `TestReferenceServiceFollowsTheProtocol`
pins the behaviours above. A service in any language is fine: the contract is
HTTP and JSON. Keep it thin — it should start a build, relay what happens and
publish the outputs; template semantics belong to ICT on the worker.

A checklist for a first implementation:

1. `GET /v1/info` returns `protocol: "1"`, a name, an honest `maxConcurrent`,
   and your phases in order (or none).
2. `POST /v1/builds` writes the template and `files` where the worker will run,
   starts the build, and returns `202` with the state; the same `id` again
   returns the same build.
3. Capture the worker's console as numbered lines; serve them as `log` events
   from `from` onwards; emit `phase` when your pipeline moves on and `meta` for
   worker name and job URL; end with `done`.
4. `POST …/cancel` stops the job and the stream ends with `done {cancelled}`.
5. Keep finished builds, their artifacts' URLs and their logs for as long as
   you want them visible in the Web UI's history.
