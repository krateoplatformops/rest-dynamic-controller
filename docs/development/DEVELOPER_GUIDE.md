# Developer Guide

This guide is for people **working on** the rest-dynamic-controller (RDC) codebase. It explains the architecture, the reconciliation model, how a CR is turned into REST calls, how the code is organized, and how to build, run, test, and extend the controller locally.

If you are a *user* (writing `RestDefinition`s and managing resources), start with the [README](../../README.md) and the documentation of the [oasgen-provider](https://github.com/krateoplatformops/oasgen-provider), which is what generates the CRDs that RDC reconciles.

## Table of contents

- [Mental model: what RDC actually does](#mental-model-what-rdc-actually-does)
- [Repository layout](#repository-layout)
- [The reconciliation lifecycle](#the-reconciliation-lifecycle)
- [Resolving "client info" (the Getter)](#resolving-client-info-the-getter)
- [Building and executing a REST call](#building-and-executing-a-rest-call)
- [Comparison and status population](#comparison-and-status-population)
- [`findby` and pagination](#findby-and-pagination)
- [Authentication](#authentication)
- [How RDC and oasgen-provider fit together](#how-rdc-and-oasgen-provider-fit-together)
- [Local development workflow](#local-development-workflow)
- [Testing](#testing)
- [How to extend the controller](#how-to-extend-the-controller)
- [Conventions & gotchas](#conventions--gotchas)
- [Glossary](#glossary)

---

## Mental model: what RDC actually does

RDC is a **generic Kubernetes controller**. Unlike a typical operator, it has no compiled-in API types and no hand-written reconcile logic for a specific resource. Instead, it reconciles instances of an *arbitrary*, dynamically-generated CRD against an *arbitrary* REST API, driven entirely by:

- a **`RestDefinition`** (authored by the user/platform and reconciled by the oasgen-provider — *not* created by it) that describes the verbs, identifiers, and configuration of the resource, and
- the **OpenAPI document** that the RestDefinition points at.

One RDC process watches exactly **one GVR** (Group/Version/Resource), passed in via `REST_CONTROLLER_*` env vars / flags. The oasgen-provider deploys **one RDC instance per RestDefinition**.

```
   user creates / edits / deletes
   a "RestResource" (instance of a       ┌───────────────────────────────────────────────────────────────────┐
   generated CRD, e.g. a Repository) ───▶│ rest-dynamic-controller (this repo)                               │
                                         │                                                                   │
                                         │ Observe/Create/Update/Delete on                                   │
                                         │ *unstructured.Unstructured                                        │
                                         │   1. resolve RestDefinition + OAS + config   (Getter)             │
                                         │   2. build an HTTP client from the OAS       (restclient)         │
                                         │   3. build the call for the action           (builder)            │
                                         │   4. execute the HTTP request                                     │──▶ external REST API
                                         │   5. compare spec vs remote, populate status (comparison/helpers) │
                                         └───────────────────────────────────────────────────────────────────┘
```

RDC is built on Krateo's **`unstructured-runtime`** (not controller-runtime's typed reconciler). The reconcile loop, work queue, rate limiting, and resync are provided by that library; RDC supplies an `ExternalClient` implementation (the `handler`) and a `Getter`.

---

## Repository layout

```
main.go                          # entrypoint: flags/env, builds the unstructured-runtime controller + handler
internal/
  controllers/
    restResources.go             # THE handler: Observe/Create/Update/Delete over an unstructured CR
    helpers.go                   # isCRUpdated, populateStatusFields, clearStatusFields
    condition/                   # custom conditions (e.g. Pending() for async operations)
    mockserver/                  # standalone configurable HTTP mock used by integration tests
  tools/
    definitiongetter/            # ★ the Getter: CR → RestDefinition + OAS URL + Configuration + auth closure
    client/
      restclient.go              # the REST client: Call / FindBy / CallForPagination, status-code validation, debug transport
      clienttools.go             # UnstructuredClient, URL building, OAS-driven param/body discovery, BuildClient
      validators.go              # StatusError + status-code helpers
      apiaction/                 # the action enum (create/update/delete/get/findby/list)
      builder/                   # APICallBuilder, BuildCallConfig, IsResourceKnown — turns an action+CR into a call
    comparison/                  # spec-vs-remote comparison engine (CompareExisting, DeepEqual, InferType)
    pagination/                  # Paginator interface + continuationToken implementation
    auth/                        # auth-type enum (basic/bearer)
    filegetter/                  # fetch the OAS from configmap:// or http(s)://
    deepcopy/                    # JSON value deep-copy + numeric normalization (used when writing status)
    pathparsing/                 # dot/bracket JSON path parsing (escaping dots in field names)
  text/                          # string helpers (identifier casing, string set, generic stringer)
testdata/                        # CRDs, RestDefinitions, sample CRs, and OAS fixtures for integration tests
scripts/                         # build.sh (ko → kind) and test.sh (coverage)
```

The two most important files to understand are **`internal/controllers/restResources.go`** (the reconcile handler) and the **`internal/tools/client`** package (how a CR becomes an HTTP request). The **`internal/tools/comparison`** package is where most subtle behavior (drift detection) lives.

---

## The reconciliation lifecycle

The `handler` in `restResources.go` implements the `unstructured-runtime` `ExternalClient` interface: `Observe`, `Create`, `Update`, `Delete`. Every phase follows the same opening moves:

1. `swaggerInfoGetter.Get(mg)` → resolve the `clientInfo` (RestDefinition + OAS URL + Configuration + a `SetAuth` closure). See [Resolving client info](#resolving-client-info-the-getter).
2. `restclient.BuildClient(...)` → fetch + parse the OAS and construct an `UnstructuredClient`.
3. Set per-resource flags on the client (`Debug` from the `connector-verbose` annotation, `PrettyJSON`, auth, the resource itself, identifier policy).
4. `builder.APICallBuilder(cli, clientInfo, <action>)` → get a callable closure + call metadata for the action.
5. `builder.BuildCallConfig(...)` → assemble the request inputs.
6. Execute the call and act on the result.

### `Observe` — the decision tree

`Observe` is the most involved phase. It decides whether the external resource exists and is up to date:

```
Observe(cr):
  resolve clientInfo; build client
  IsResourceKnown(cli, clientInfo, cr)?           # can a GET-by-id request be fully built from spec+status?
    ├─ yes → call the `get` action (e.g. GET /resources/{id})
    └─ no  → call the `findby` action (list + match by identifiers)
             └─ no findby call available?
                  ├─ not yet Creating/Available → "being created", stop
                  └─ else → assume up-to-date (no get/findby to verify against)
  on 404:
    ├─ + Pending condition → exists & up-to-date (async create in flight)
    └─ otherwise           → does NOT exist  → reconciler will call Create
  response body nil (e.g. 204/304)  → assume up-to-date
  else:
    populateStatusFields(clientInfo, cr, body)    # write identifiers + additionalStatusFields into status
    isCRUpdated(cr, body)?                         # compare spec vs remote
      ├─ equal     → Available, UpToDate
      └─ not equal → Unavailable, NOT UpToDate     → reconciler will call Update
```

The **known vs findby** split is the core idea: a resource is "known" once a direct `get`-by-id request can be fully built — i.e. the `get` operation's required parameters (typically the path `{id}`) can be filled from the CR's `spec`/`status`. This check (`IsResourceKnown` → `ValidateRequest`) looks **only** at whether the required request parameters are satisfiable; it does **not** consult the configured `identifiers`. Before the resource is known — e.g. on the first reconcile of a resource whose ID is server-assigned — RDC must instead *find* it in a list via `findby`, and **`findby` is the only place `identifiers` are used to locate the resource** (matched per `IdentifiersMatchPolicy`, `AND`/`OR`). A resource that supports only `findby` (no `get`) also takes this branch.

> The `identifiers` list drives exactly two things: **`findby` matching** (`isItemMatch`) and **status population** (`populateStatusFields` copies the identifier values from the response into `status`). It plays **no role** in building the `get`-by-id request — that is constructed purely from the OAS operation's required parameters, sourced from `spec`/`status` (plus Configuration / field mappings). The identifier *values* may of course end up in the `get` path because they live in `spec`/`status`, but the `identifiers` list itself is not consulted there.

### `Create` / `Update`

Both build and execute the corresponding action, then populate status from the response:

- `Create` **clears status first** (`clearStatusFields`) so stale identifiers from a previously-deleted external resource can't cause a reconcile deadlock, then populates from the create response (if any).
- `Update` clears+repopulates only when the response has a body; an empty-body response keeps existing status.
- Both set a condition: `Pending` if the response is an async/pending status, otherwise `Creating`. (There is no dedicated `Update` condition; `Creating` is reused.)

### `Delete`

`Delete` builds the `delete` action and calls it. It is defensive about missing prerequisites: if the RestDefinition/Configuration can't be resolved, or the CR has no `status` yet (never created externally), it sets `Deleting` and lets the CR be removed **without** attempting the external call.

---

## Resolving "client info" (the Getter)

`internal/tools/definitiongetter` turns a CR into everything needed to call the API. `getter.Dynamic(...)` constructs the implementation; `Get(cr)`:

1. Lists `RestDefinition`s and finds the one whose `resource.kind` + `resourceGroup` match the CR's kind/group.
2. Extracts the `resource` block (verbs, identifiers, additional status fields) and the `oasPath`.
3. Resolves the CR's `configurationRef` → the `<Kind>Configuration` CR, reads its `spec.configuration` (per-action parameters) and `spec.authentication`.
4. Resolves auth secret references and produces a `SetAuth func(*http.Request)` closure (basic or bearer).

The result is an `Info` struct carrying `URL` (the OAS location), `Resource`, `ConfigurationSpec`, and `SetAuth`. Note the **request base URL is not `Info.URL`** — it is the first `server` entry in the parsed OAS; `Info.URL` is only where the OAS document itself is fetched from.

---

## Building and executing a REST call

### From action to call: the `builder` package

`builder.APICallBuilder(cli, clientInfo, action)` looks up the `VerbsDescription` matching the action and returns a closure that performs the HTTP request, plus `callInfo` (method, path, the OAS-derived parameter/body sets). For `get`/`findby` it returns the appropriate read closure; for write actions it includes the request body for `POST`/`PUT`/`PATCH`.

`builder.BuildCallConfig(callInfo, cr, configurationSpec)` assembles the actual request inputs, with this **precedence** (later sources do not overwrite values already set by earlier ones):

1. **Configuration CR** (`applyConfigSpec`) — per-action path/query/header/cookie values. Headers and cookies can *only* be set here.
2. **Explicit `requestFieldMapping`** (`applyRequestFieldMapping`) — maps a CR field (by JSON path) to a path param, query param, or body field. The body path preserves value types; path/query are stringified.
3. **CR `spec`** fields.
4. **CR `status`** fields.

`builder.IsResourceKnown` decides the Observe known/findby branch by attempting to build the `get` call config and checking whether `ValidateRequest` passes (i.e. all required path params are satisfiable).

### Executing: the `client` package

`restclient.BuildClient(ctx, dynamicClient, oasURL)` fetches the OAS (via `filegetter`), parses it with libopenapi, and returns an `UnstructuredClient`. The client then exposes:

- `Call` — the standard single request: builds the URI (`buildPath`), validates required params (`ValidateRequest`), assembles the JSON body, injects auth via `SetAuth`, performs the request, validates the status code against the OAS-declared responses (`getValidResponseCode` / `HasValidStatusCode`), and parses the body.
- `FindBy` / `CallFindBySingle` / `CallForPagination` — list-and-match for the `findby` action (see below).
- `debuggingRoundTripper` — when verbose mode is on, dumps the full request/response for debugging.

Status-code handling allows empty bodies for a set of "no content"-style codes (e.g. 204/304) and treats certain codes as **pending** (async). The accepted success codes come from the OAS responses for the operation.

---

## Comparison and status population

After a successful read, `Observe` does two things with the response body, both in `internal/controllers/helpers.go`:

- **`populateStatusFields`** — for each `identifier` + `additionalStatusField`, parse its path (`pathparsing`), extract the value from the response, deep-copy/normalize it (`deepcopy`), and write it to the mirrored path under `status`.
- **`isCRUpdated`** — extract the CR `spec` and compare it against the remote body via `comparison.CompareExisting`.

### How comparison works

`comparison.CompareExisting(spec, remote)` is **asymmetric and one-directional**: it walks only the keys present in the CR `spec` (the desired state). Keys present in the remote but not in the spec are ignored — so a resource can have many server-managed fields without being considered "drifted". For each spec key it recurses into maps, compares slices **positionally** (order matters), and for scalars uses `CompareAny`.

`CompareAny` and `DeepEqual` normalize values through `InferType` before comparing: a value is stringified and then re-parsed into the most appropriate Go type (number/bool/null/string). This is what lets a spec value of `5` compare equal to a remote `5.0`, and it includes a guard so identifier-like strings that *start* with digits (e.g. UUIDs `90f9629b-...`) are treated as whole strings rather than partially parsed as numbers.

`DeepEqual` (symmetric, nil-normalizing) is used separately by the `findby` matching path (`isInResource`) to decide whether a listed item matches the CR's identifiers.

> When you touch comparison, keep in mind it is the single most behavior-sensitive part of RDC: it decides "up-to-date", which decides whether `Update` fires. Add table-driven tests for any change (see `comparison_test.go` for the established style).

---

## `findby` and pagination

`findby` is used when the resource is not directly addressable by id yet (or only supports list). `FindBy`:

1. Builds the list request and (optionally) a `Paginator`.
2. Calls the list endpoint, extracts the array of items from the response (`extractItemsFromResponse` — handles a top-level array, an object wrapping an array, or a single object), and tries to match an item against the CR's identifiers (`isItemMatch`, honoring the `AND`/`OR` `identifiersMatchPolicy`).
3. If no match and pagination is configured, advances to the next page and repeats; otherwise returns a not-found (`404`) error.

Pagination lives in `internal/tools/pagination` behind a `Paginator` interface (`Init` / `UpdateRequest` / `ShouldContinue`). The only implemented strategy is `continuationToken` (token carried in a request query parameter / response header). The interface is designed so additional strategies (page number, offset) can be added without touching `FindBy`.

---

## Authentication

Auth is resolved by the Getter from the Configuration CR's `authentication` block and injected per-request by the `SetAuth` closure. Supported schemes (`internal/tools/auth`): HTTP **basic** and **bearer**. Secret values are read from Kubernetes Secrets referenced in the Configuration and decoded before being attached to the request (`req.SetBasicAuth` / `Authorization: Bearer <token>`).

> Security note for developers: the debug round-tripper dumps the full HTTP request, which includes the `Authorization` header. Be careful when adding or extending debug output — redact credentials, and remember that verbose mode is user-togglable via a CR annotation. Likewise, treat the OAS URL, the `configurationRef`/secret namespaces, and `configmap://` sources as untrusted input (SSRF / cross-namespace reads / response size).

---

## How RDC and oasgen-provider fit together

RDC and [oasgen-provider](https://github.com/krateoplatformops/oasgen-provider) form one product:

- **oasgen-provider** parses the OAS, generates the CRD (+ Configuration CRD), and **deploys an RDC instance** (Deployment + ConfigMap + RBAC) configured for that resource's GVR.
- **RDC** (this repo) reconciles instances of that generated CRD against the REST API at runtime.

Several behaviors are **shared contracts that must agree on both sides** — when you change one, check the other:

- **Success codes & response shapes.** oasgen validates the generated schemas against a set of success codes; RDC accepts only the OAS-declared codes at call time. An API returning `202`, or a `200` with an empty body, can fall between the two. Coordinate changes to success-code/empty-body handling across both repos.
- **Response codes as map keys.** Both sides currently key OAS responses by integer status code, so `default` and `2XX`-range responses are not handled. Same caveat applies to both.
- **Identifiers / status contract.** oasgen decides which response fields become `status`; RDC populates and compares them. Their notion of the status shape must match.
- **libopenapi fork.** Both repos `replace` `pb33f/libopenapi` with the Krateo fork. Keep the fork version in sync so the OAS parses identically at generation time (oasgen) and at runtime (RDC).

---

## Local development workflow

### Prerequisites

- Go **1.25.3+** (matches `go.mod`; with an older local toolchain use `GOTOOLCHAIN=auto`).
- Docker, [`kind`](https://kind.sigs.k8s.io/), `kubectl`, [`ko`](https://ko.build/), and `jq`.

### Build into a kind cluster

```sh
./scripts/build.sh    # ko build the image into the local registry / kind
```

> Note: `scripts/build.sh` contains a hardcoded `KO_DOCKER_REPO` value (a developer's personal repo). Override `KO_DOCKER_REPO` (e.g. `KO_DOCKER_REPO=kind.local`) for your own environment; don't rely on the committed default.

### Running RDC

RDC is normally **deployed by the oasgen-provider**, one instance per RestDefinition, and is configured entirely through flags / `REST_CONTROLLER_*` env vars (see `main.go`). The most important ones:

| Flag | Env var | Purpose |
|---|---|---|
| `--group` / `--version` / `--resource` | `REST_CONTROLLER_GROUP` / `_VERSION` / `_RESOURCE` | the single GVR this instance watches |
| `--namespace` | `REST_CONTROLLER_NAMESPACE` | namespace to watch (empty = all) |
| `--workers` | `REST_CONTROLLER_WORKERS` | concurrent reconcile workers |
| `--resync-interval` | `REST_CONTROLLER_RESYNC_INTERVAL` | periodic resync |
| `--debug` | `REST_CONTROLLER_DEBUG` | debug logging |
| `--pretty-json-debug` | `REST_CONTROLLER_PRETTY_JSON_DEBUG` | pretty-print JSON bodies in debug dumps |
| `--metrics-server-port` | `REST_CONTROLLER_METRICS_SERVER_PORT` | enable metrics (0 = disabled) |
| `--min/max-error-retry-interval`, `--max-error-retries` | `REST_CONTROLLER_*` | error backoff & retry budget |

To run a single instance locally against a cluster (pointing at one already-generated CRD):

```sh
go run . --kubeconfig "$HOME/.kube/config" \
  --group <group> --version v1alpha1 --resource <plural> \
  --namespace default --debug
```

---

## Testing

### Unit tests

```sh
go test -cover -v ./...
# or:
./scripts/test.sh        # coverage profile + per-func summary
```

The richest unit suites are in `internal/tools/comparison`, `internal/tools/client` (+ `builder`), and `internal/controllers/helpers_test.go`. They are table-driven and use `httptest` (recorders + custom transports) to exercise the REST client without a live server. **When you change call-building, comparison, or status population, add a table-driven case** — that is the established pattern and the fastest way to lock in behavior.

### The mock server

`internal/controllers/mockserver` is a standalone, configurable HTTP server (toggleable errors, auth, delays, async responses) used to drive realistic request/response flows in tests. It's useful when reproducing a tricky API behavior locally.

### Integration tests

```sh
go test -tags=integration -cover -v ./...
```

Integration tests (gated by the `integration` build tag) provision a real kind cluster and exercise the handler end-to-end against the mock server, using the CRDs and sample CRs under `testdata/`. They require Docker + kind and are slow.

> If you add scenarios, prefer condition polling over fixed `time.Sleep`, and avoid host-specific tooling so the tests stay portable across developer machines and CI.

---

## How to extend the controller

A few common tasks and where they touch:

### Support a new request-input source or mapping

- Work in `internal/tools/client/builder` (`BuildCallConfig` and the `apply*` helpers). Respect the existing precedence (Configuration → field mapping → spec → status) and the "don't overwrite already-set values" rule.
- Preserve value types where the API needs them (the body path already does; path/query are stringified).

### Add a pagination strategy

- Implement the `Paginator` interface in `internal/tools/pagination` and wire it into the constructor. `FindBy` should not need changes. Add a guard against infinite loops (e.g. repeated/identical tokens or a page cap).

### Add an authentication scheme

- Add the type to `internal/tools/auth`, resolve it in the Getter (`definitiongetter`), and extend the `SetAuth` closure to inject it. Coordinate the corresponding schema with oasgen-provider, which generates the Configuration CRD that carries the auth fields.

### Change comparison / "up-to-date" semantics

- Work in `internal/tools/comparison`. This is behavior-critical (it gates `Update`); add table-driven tests and be deliberate about type normalization, slice ordering, and the asymmetric (spec-only) traversal.

### Change status population

- Work in `populateStatusFields` (`internal/controllers/helpers.go`) and the `deepcopy`/`pathparsing` helpers. Surface failures (bad path, missing field) rather than silently skipping, so misconfigurations are visible.

---

## Conventions & gotchas

- **RDC has no compiled-in API types.** It operates on `*unstructured.Unstructured`; the CRD it reconciles is generated by oasgen-provider. There is no `apis/` package and no `controller-gen` step here.
- **One process = one GVR.** Behavior is driven entirely by env/flags; multiple resources mean multiple RDC Deployments (managed by oasgen).
- **The request base URL comes from the OAS `servers`, not from `Info.URL`.** `Info.URL` is only the OAS document's location.
- **Comparison is asymmetric and order-sensitive.** It walks spec keys only; slices are compared positionally. This is intentional but surprising — keep it in mind before "fixing" a comparison that looks too lenient or too strict.
- **Prefer surfacing failures over silent skips.** Several paths currently `continue` on parse/lookup errors; new code should log or signal so misconfigurations (bad identifier paths, unresolved mappings, unsupported auth, unimplemented pagination modes) are observable.
- **Treat all external input as untrusted.** The OAS URL, the target server, secret/`configmap` namespaces, and response bodies are user-influenced — be mindful of SSRF, cross-namespace reads, response size, and credential logging when working in `filegetter`, the Getter, or the debug transport.
- **Two repos, one contract.** Success codes, response shapes, identifiers, and the libopenapi fork must stay aligned with oasgen-provider.

---

## Glossary

- **RestDefinition** — the CR describing a resource's OAS, verbs, identifiers, etc. It is authored by the user/platform and reconciled by the oasgen-provider (which generates the CRD from it); RDC reads it to learn how to call the API.
- **RestResource** — an instance of the *generated* CRD (e.g. a `Repository`); this is what RDC reconciles.
- **Configuration CR** — the auxiliary CR holding per-action parameters and authentication for a RestResource.
- **clientInfo / `Info`** — the resolved bundle (OAS URL, resource config, Configuration spec, `SetAuth`) the handler builds per reconcile via the Getter.
- **Verb / action** — `create`, `get`, `update`, `delete`, `findby`; mapped to OAS operations.
- **Identifiers** — fields used by `findby` to match the right item in a list, and by `populateStatusFields` to copy values into `status`. They are **not** used to build the `get`-by-id request (which relies on the operation's required path/query parameters).
- **known vs findby** — the Observe branch: "known" = the `get`-by-id request's required parameters are satisfiable from `spec`/`status` (identifiers are not consulted); otherwise locate the resource via a `findby` list + identifier match.
- **Pending** — a custom condition marking an in-flight async create/update (e.g. an API that returns `202`).

---

*This developer guide describes how the controller is intended to work. For user-facing documentation, see the [README](../../README.md) and the [oasgen-provider](https://github.com/krateoplatformops/oasgen-provider) documentation.*
