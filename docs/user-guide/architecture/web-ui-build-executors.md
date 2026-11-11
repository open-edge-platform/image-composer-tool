# Web UI build executors: the `Executor` seam

**Who this is for.** Contributors who want `image-composer-tool serve` to run
builds somewhere other than its own host, reviewers of such a change, and
anyone reading `internal/api/service` who wants to know where a build backend
plugs in.

**In one sentence.** `service.Executor` is the single interface between the Web
UI build service and whatever runs a build. Everything on either side of it —
the HTTP API, the live log stream, build history, cancellation and the Web UI —
is shared by every backend, so a new backend is one type that implements three
methods, not a fork of the service.

**Where the code is.**

| What | Where |
|---|---|
| The seam: `Executor`, `PhaseReporter`, `ExecutorHandle`, `Phase` | `internal/api/service/executor.go` |
| The dispatch: `runViaExecutor`, cancel bookkeeping | `internal/api/service/executor.go` |
| The default (no executor): `runBuild` spawns the ICT binary | `internal/api/service/builds.go` |
| Capacity: `Config.MaxConcurrent`, the slot set | `internal/api/service/service.go` |
| A fake executor and the seam's tests | `internal/api/service/executor_test.go` |

## Where the seam sits

```mermaid
flowchart LR
    B[Browser<br/>Web UI] -->|REST + SSE| API[internal/api<br/>HTTP handlers]
    API --> SVC[service.Service<br/>builds, history, cancel, log buffer]
    SVC --> RB{Config.Executor<br/>set?}
    RB -->|no: today's behaviour| LOCAL[runBuild<br/>spawns image-composer-tool build<br/>on this host]
    RB -->|yes| EX[Executor.Run<br/>your backend]
    EX -.->|AppendLog / SetPhase / PutMeta| SVC
```

`Config.Executor == nil` keeps today's behaviour byte for byte. With an
executor set, `runViaExecutor` is the only code that knows one exists.

## The seam as a class diagram

```mermaid
classDiagram
    direction LR

    class Service {
        -cfg Config
        -activeBuilds set of build ids
        -executorCancels map of build id to cancel func
        +StartBuild(req BuildRequest) BuildAccepted
        +CancelBuild(id string) CancelAccepted
        +MaxConcurrent() int
        +ExecutorName() string
        +ExecutorPhases() List~Phase~
        -runBuild(b build)
        -runViaExecutor(b build)
    }

    class Config {
        +Executor Executor
        +MaxConcurrent int
    }

    class Executor {
        <<interface>>
        +Name() string
        +Run(ctx Context, h ExecutorHandle) BuildStatus, List~Artifact~, error
        +Cancel(ctx Context, h ExecutorHandle) error
    }

    class PhaseReporter {
        <<interface>>
        +Phases() List~Phase~
    }

    class Recoverer {
        <<interface>>
        +Recover(ctx Context, buildID string) Recovered, error
    }

    class Recovered {
        +Status BuildStatus
        +ErrMsg string
        +Log List~string~
        +Artifacts List~Artifact~
        +Meta map of string to string
    }

    class ExecutorHandle {
        <<interface>>
        +ID() string
        +TemplatePath() string
        +TemplateYAML() string, error
        +WorkDir() string
        +AppendLog(line string)
        +SetPhase(p Phase)
        +PutMeta(key string, value string)
    }

    class executorHandle {
        -b build
    }

    class build {
        +ID string
        +Status BuildStatus
        -logs List~string~
        -phase Phase
        -meta map of string to string
        -finish(status BuildStatus, arts List~Artifact~, errMsg string)
    }

    class Phase {
        +ID string
        +Label string
    }

    class Artifact {
        +Name string
        +Type string
        +Path string
        +Size string
        +URL string
        +SHA256 string
    }

    class YourExecutor {
        a type you write
    }

    Service --> Config : cfg
    Config o-- "0..1" Executor : nil means local
    Service ..> ExecutorHandle : creates one per build
    executorHandle ..|> ExecutorHandle
    executorHandle --> build : adapts
    Executor ..> ExecutorHandle : receives
    Executor ..> Artifact : returns
    ExecutorHandle ..> Phase : SetPhase
    YourExecutor ..|> Executor : required
    YourExecutor ..|> PhaseReporter : optional
    YourExecutor ..|> Recoverer : optional
    Recoverer ..> Recovered : returns
```

How to read it:

- **`Executor`** is what you implement. `Run` drives one build to completion
  and returns how it ended; `Cancel` asks it to stop. `Name` is shown in the
  startup banner and in build details.
- **`ExecutorHandle`** is the executor's view of one build: read the template,
  publish log lines, report a phase, record backend detail. It deliberately
  has **no** way to set the build's terminal status; only `Run`'s return value
  does, so there is exactly one path to completion.
- **`PhaseReporter`** is optional. If your executor knows the ordered phases its
  builds go through, the Web UI draws its stepper from them instead of from the
  fixed local phases.
- **`Recoverer`** is optional. If your executor can read a build back by id, a
  restarted server picks up builds the previous instance left running instead
  of recording them as interrupted (see [Recovering builds after a
  restart](#recovering-builds-after-a-restart)).
- **`Config.Executor`** is how `serve` plugs an executor in; **`MaxConcurrent`**
  is how many builds the service lets run at once (default 1).

## A build through an executor

```mermaid
sequenceDiagram
    autonumber
    actor U as Browser (Web UI)
    participant API as API (internal/api)
    participant SVC as Service
    participant EX as Executor (your backend)
    participant SSE as SSE handler (sse.go)

    U->>API: POST /api/v1/builds
    API->>SVC: StartBuild(req)
    SVC->>SVC: tryAcquireBuildSlot(id) — one of MaxConcurrent
    SVC->>SVC: go runBuild(b)
    SVC-->>API: BuildAccepted {buildId, logsUrl}
    API-->>U: 202 Accepted

    Note over SVC: Config.Executor != nil → runViaExecutor(b)
    SVC->>SVC: b.markRunning() · b.putMeta("executor", Name())
    SVC->>EX: Run(ctx, executorHandle{b})

    U->>SSE: GET /api/v1/builds/{id}/logs (EventSource)
    loop while the build runs
        EX->>SVC: h.AppendLog(line) — one line at a time, as it happens
        EX->>SVC: h.SetPhase({id, label}) · h.PutMeta(key, value)
        SSE->>SVC: snapshotLogs(), ReportedPhase()
        SSE-->>U: event log {message} · event phase {phase, label}
    end

    EX-->>SVC: return status, artifacts, err
    SVC->>SVC: b.archiveAndCleanupDelta()
    SVC->>SVC: b.finish(status, artifacts, errMsg) → meta.json, log file
    SVC->>SVC: releaseBuildSlot(id)
    SSE-->>U: event phase {done} · event complete {status, artifacts}
```

Points that matter when you implement `Run`:

1. **Stream the log.** Call `h.AppendLog` as lines arrive. The SSE handler
   publishes whatever is in the buffer on each tick; a `Run` that appends
   everything at the end looks hung for the life of the build.
2. **A build that ran and failed is not an error.** Return `StatusFailed` with a
   `nil` error so its log and partial artifacts are kept. Return a non-nil
   error only when the build could not be run at all (the service records it as
   failed with your message).
3. **Return `StatusCancelled`** when the build stopped because `Cancel` was
   called. If you return `StatusFailed` after a cancel was requested, the
   service records `cancelled` anyway: the user's intent wins over the symptom.
4. **Artifacts** may live on this host (`Path`, under `h.WorkDir()`) or
   elsewhere (`URL`, with `SHA256`). Both are recorded in `meta.json`.

## Cancelling an executor build

```mermaid
sequenceDiagram
    autonumber
    actor U as Browser (Web UI)
    participant API as API (internal/api)
    participant SVC as Service
    participant EX as Executor (your backend)

    U->>API: POST /api/v1/builds/{id}/cancel
    API->>SVC: CancelBuild(id)
    SVC->>SVC: b.beginCancel() → status cancelling
    SVC->>EX: Cancel(ctx, executorHandle{b})
    alt Cancel returns an error
        SVC->>SVC: record it on the build (residual) and in the log
    end
    SVC->>SVC: cancel the ctx that Run is blocked on
    SVC-->>API: CancelAccepted {status: cancelling}
    API-->>U: 202 Accepted
    Note over EX,SVC: Run is expected to return soon after
    EX-->>SVC: Run returns status, artifacts, err
    SVC->>SVC: failed after a cancel request → cancelled · b.finish(...)
```

`Cancel` may return before the build has actually stopped. The service cancels
the context it passed to `Run` as well, so an executor that only watches the
context still stops. There is no watchdog on this path: `runViaExecutor` records
the terminal state when `Run` returns, so `Run` **must** return once the build
has stopped.
ICT's remote executor, for instance, keeps following the build for a bounded
grace period after a cancel so the real outcome is recorded rather than assumed.

## Phases, metadata and artifacts: how they reach the UI

| You call | The service stores it as | Where it surfaces |
|---|---|---|
| `h.AppendLog(line)` | the build's log buffer and, at the end, `build.log` | SSE `log` events; `GET /api/v1/builds/{id}/logfile` |
| `h.SetPhase(Phase{ID, Label})` | the build's reported phase (`ReportedPhase()`) | SSE `phase` events (`phase`, `label`) |
| `h.PutMeta(key, value)` | `meta.json` → `meta` map (`worker`, `url`, …) | `BuildDetails.Meta`, `HistoryItem.Meta` in the service |
| `Phases()` (PhaseReporter) | nothing; read on demand | `Service.ExecutorPhases()` |
| `Run`'s return value | status, artifacts, error in `meta.json` | SSE `complete`/`error`; details; history; artifact list |

The SSE handler prefers an executor-reported phase over the log-derived one;
when an executor never calls `SetPhase`, the local phase detection runs over the
log lines exactly as before.

## Capacity and shutdown

- The single build slot is a **bounded set** of `Config.MaxConcurrent` slots
  (default 1). `StartBuild` answers `409` once they are all held.
- The local executor must keep `MaxConcurrent == 1`: local builds share this
  host's mounts and loop devices.
- **Shutdown leaves executor builds running.** They are not this host's child
  processes, so `CancelInFlightBuilds` returns early when an executor is set.
  A restarted server recovers them (next section).

## Rules for an `Executor` implementation

- `Name()` is a short, stable, human-readable name (`"local"`, `"farm-1"`).
- `Run` blocks for the life of the build, honours `ctx`, streams the log, and
  returns exactly once with a terminal status.
- `Cancel` is idempotent and safe to call for a build that has already finished.
- `Run` and `Cancel` may be called concurrently for different builds; the
  service holds no lock while calling either.
- Use `network.NewSecureHTTPClient()` rather than `http.DefaultClient` for any
  HTTP your executor does (see the coding style guide).
- Implement `PhaseReporter` when your backend has well-defined phases; keep the
  `ID`s stable, the `Label`s human.
- Implement `Recoverer` when your backend can be asked about a build by the id
  the service gave it; it is what makes a `serve` restart harmless.

## Recovering builds after a restart

A build record whose status is still `running` when `serve` starts means the
previous instance went away mid-build. For the local executor that build is
lost (its child process died with the server). For an executor that implements
`Recoverer`, the service asks what became of each such build and brings the
record to its real state, once, before the server starts serving.

```mermaid
sequenceDiagram
    autonumber
    participant SRV as Server.Start
    participant SVC as Service
    participant EX as Executor (Recoverer)

    SRV->>SVC: RecoverExecutorBuilds(ctx)
    SVC->>SVC: listRunningOnDisk() — meta.json with status running
    loop for each such build
        SVC->>EX: Recover(ctx, buildID) with a 30 s timeout
        alt finished meanwhile
            EX-->>SVC: Recovered {status, log, artifacts, meta}
            SVC->>SVC: replay log, meta · b.finish(status, artifacts, errMsg)
        else still running (ErrStillRunning)
            EX-->>SVC: ErrStillRunning
            SVC->>SVC: tryAcquireBuildSlot(id) · tracker.add(b)
            SVC->>SVC: go runBuild(b) → Run follows the build again
            Note over SVC,EX: Run replays the whole log so late SSE subscribers see it all
        else unknown (error)
            EX-->>SVC: error
            SVC->>SVC: b.finish(failed, "server restarted · build outcome unknown")
        end
    end
    SRV->>SRV: start listening
```

A build that is still running is **re-adopted**: it takes a slot and `Run` is
called for it exactly as for a new build, so cancel, SSE and history work for
it unchanged. If no slot is free, the build is left as it is and a warning is
logged; it will be recovered on the next start.

## The executor ICT ships

`internal/api/executor/remote` implements `Executor`, `PhaseReporter` and
`Recoverer` over the [remote build protocol](./remote-build-protocol.md): an
HTTP+SSE contract that any build service, in any language, can implement. It is
the executor behind a build farm.
Read it as a worked example of this seam, and read the protocol document for
how its `Run`, `Cancel` and `Recover` map onto the five calls a build service
exposes.

## Testing an executor

`internal/api/service/executor_test.go` has a `fakeExecutor` that scripts log
lines, phases, outcome and cancel behaviour, and tests that pin the seam's
contract: a build runs and records its outcome, `MaxConcurrent` is honoured,
cancel reaches the executor and the build ends `cancelled`, a `Run` error fails
the build with its message, the template arrives self-contained, and shutdown
leaves executor builds running. Start from it when you test your own executor;
the same tests, pointed at your type, are a conformance suite.
