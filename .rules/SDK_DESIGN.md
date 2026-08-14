# SDK Design: the Go realization of the cross-SDK contract

Load this alongside [`SDK_CONTRACT.md`](SDK_CONTRACT.md) when adding a **resource, verb,
response model, paginated listing, or webhook event**.

`SDK_CONTRACT.md` is the shared, language-neutral constitution: configuration, layering,
naming, response-model rules, pagination, the error model, and the webhook contract, all of
which every mailkube SDK implements identically. It is shared verbatim across every mailkube SDK
and must not be edited here.

**This file covers only what is specific to Go**, including the deliberate deviations the
contract explicitly allows.

## The layers, in files

The package lives at the module root, so the layers are files rather than directories.

| Layer | Files | May know about |
|---|---|---|
| **Client / IO** | `transport.go` | `net/http` |
| **Core** | `client.go` (config, URL building, headers, redirect policy), `errors.go`, `logging.go`, `serialization.go` | nothing I/O-specific |
| **Services** | `emails.go`, `scheduled_emails.go` | a transport interface plus its own request shaping |
| **Types** | `types.go`, `events.go` | nothing |
| **Public helpers** | `webhooks.go` (verification), `webhooks_http.go` (the `http.Handler`) | `net/http` |

`client.go` is the composition root. Only `transport.go` and `webhooks_http.go` perform or serve
I/O; a service file must never import `net/http` for anything but its method constants.

## Zero runtime dependencies

`go.mod` has no `require` block, and it should stay that way. The standard library covers
everything: `net/http`, `encoding/json`, `crypto/hmac`, `log/slog`, `iter`. Every consumer
inherits any dependency added here. This is also why there are no framework adapters in the
module: `WebhookHandler` is an `http.Handler`, which every framework can mount.

## Deliberate deviations

1. **Error categories are sentinels, not a type hierarchy.** The other SDKs expose one
   exception class per status. Go's convention is `errors.Is` against a sentinel, so this SDK
   exposes `ErrBadRequest`, `ErrAuthentication`, `ErrNotFound`, `ErrConflict`,
   `ErrInvalidRequest`, `ErrRateLimit`, `ErrServer` and `ErrAPI`, and a single `*APIError`
   struct carries the envelope. `ErrMailkube` is the contract's one base type, which every
   other sentinel wraps and `APIError.Is` matches. The categories and the status that selects
   each are identical to every other SDK; only the shape is idiomatic.

   ```go
   var apiErr *mailkube.APIError
   if errors.As(err, &apiErr) && errors.Is(err, mailkube.ErrRateLimit) {
       time.Sleep(time.Duration(apiErr.RetryAfter) * time.Second)
   }
   ```

2. **Functional options replace keyword parameters.** Go has neither keyword arguments nor
   optional parameters, so `New(WithAPIKey(...), WithHTTPClient(...))` is the translation.
   Verb parameters use a struct (`SendEmailParams`, `ScheduledEmailListParams`) instead,
   because they are data rather than configuration. A params struct's zero-valued field is
   omitted from the wire entirely: `listSpec` skips `page` when it is `< 1`, because the
   server requires `page >= 1` and would reject the struct's own zero value.

3. **There is one client, and it is synchronous.** The contract's sync-only rule applies:
   concurrency in Go is the caller's to express with goroutines, so a second async surface
   would be non-idiomatic duplication. The control an async client would give you is already
   here as `context.Context`, which every verb takes as its first parameter and which carries
   cancellation and deadlines. **Never add a callback- or channel-returning twin of a verb.**

   Because concurrency is handed to the caller, the contract's concurrency obligation is what
   backs that promise: **one `*Client` is safe for use from many goroutines, and
   `concurrency_test.go` proves it** by driving two different verbs concurrently and asserting
   each goroutine gets its own response. `New` finishes populating the client before it returns
   and nothing mutates it afterwards, so the guarantee is structural rather than lock-based.
   Two consequences when you add a verb: never cache per-request state on `Client`, and never
   reuse `client_test.go`'s `stubClient` from a concurrent test — its `capture` records into
   shared fields, so `-race` would fail on the helper rather than on the client.

4. **The iterate-all verb is a `iter.Seq2`, not a cursor object.** `All` returns
   `iter.Seq2[*ScheduledEmail, error]`, so the common case is a `for ... range` and a failure
   arrives as the loop's second value. Range-over-func runs on the calling goroutine, so
   abandoning the loop leaks nothing. It does require `go 1.23` in the **consumer's** `go.mod`,
   not just in their toolchain, which is why the README documents the manual `List` loop next
   to it.

5. **The event catalogue is a registry, and `Raw` carries the unknown fields.** Go has no
   discriminated union, so `eventTypes` (a `map[string]func() any`) is the catalogue, `Event.Data`
   holds the typed payload, and `EventTypes()` derives the public list from the registry so no
   second list can drift from it. The contract's "unknown fields are preserved" rule is met by
   `Event.Raw`, the verbatim body: a typed Go struct drops unknown nested fields, so a receiver
   that logs or forwards an event forwards `Raw`.

6. **A strict decoder degrades instead of failing.** pydantic coerces a drifted field and keeps
   extras; `encoding/json` does neither. So a *known* event type whose data does not fit its
   struct degrades to `*UnknownData` exactly like an unknown type, and `ParseEvent` returns an
   error only for a body that is not JSON at all. Without this, one server-side field change
   would turn every affected delivery into a 400, and a sustained 4xx rate gets the endpoint
   disabled.

7. **`time.Time` is the only accepted timestamp input.** The contract asks for "a string or the
   language's datetime type" because dynamic languages make datetimes painful to construct. Go
   is statically typed, one input keeps the params structs honest, and everything renders
   through `renderTime`. Responses still hand back the verbatim server strings, so a round trip
   costs the caller one `time.Parse(time.RFC3339, ...)`, which the docs show.

8. **Webhook verification takes a `HeaderGetter`, not a concrete header type.** `http.Header`
   satisfies the one-method interface as it stands, so net/http, chi, gin and echo pass
   `r.Header` unchanged, while fasthttp stacks wrap their own accessor in `HeaderFunc`. Taking
   `http.Header` would have excluded Fiber, which is a large part of the Go web ecosystem.

9. **Response models are mutable structs the caller must treat as read-only.** The contract
   calls for immutable models; Go has no frozen struct and no read-only reference. The SDK never
   reads a model back after returning it, so mutating one affects nothing but the caller's own
   copy, and no verb takes a response model as input.

## Go idioms that realize the contract

- **`*http.Client` is the injection seam** (`WithHTTPClient`). Tests stub it with an
  `http.RoundTripper` func, which is how the suite runs without network access.
- **The transport interfaces are unexported and one per capability** (`sendTransport`,
  `jsonTransport`). The seam exists for layering and testing; it is not public API. A new
  capability adds an interface; it never widens an existing one.
- **Typed decoding is a package-level generic** (`fetch[T]`), because Go methods cannot have
  type parameters. An empty, non-object or malformed 2xx body is `ErrUnexpectedResponse`, never
  a zero-valued model.
- **Wire bodies are tagged structs, not maps.** `omitempty` states per field whether an unset
  value is absent from the wire, and `Attachment.MarshalJSON` puts the base64 encoding on the
  type itself. The regression guard for a body change is the **key set**, not the byte sequence:
  `encoding/json` sorts map keys but emits struct fields in declaration order.
- **Interpolated path segments go through `itemPath`**, which escapes them, so an id carrying
  an encoded `/` or `?` stays one segment instead of re-targeting the request.
- **Timestamps stay strings** on responses; the SDK does not reinterpret server data.
- **Logging is silent by construction.** `resolveLogger` returns a `discardHandler` when nothing
  asked for output, so `c.logger` is never nil and no call site carries a nil check.
  `slog.DiscardHandler` would do the same job but only exists in Go 1.24, and the matrix
  includes 1.23.
- **`Version()` reads `runtime/debug.ReadBuildInfo`**, never a literal, so the reported version
  equals the version the consumer actually built against. It reads `0.0.0` from the module's
  own source tree, which is expected.

## Where the shared rules are enforced

| Contract rule | Enforced in |
|---|---|
| Key/base-URL resolution | `New` in `client.go` |
| Default headers, Content-Type only with a body | `Client.defaultHeaders`, `Client.newRequest` |
| Origin guard and URL joining | `Client.buildURL` |
| Redirects never followed | `refuseRedirect`, set by `New` |
| One place maps non-2xx to an error | `Client.do` calling `apiErrorFrom` |
| A malformed 2xx is not an API error | `fetch` and `Client.sendEmail` returning `ErrUnexpectedResponse` |
| Status-to-category table | `statusSentinels` / `sentinelFor` in `errors.go` |
| One base error type | `ErrMailkube`, wrapped by every sentinel and matched by `APIError.Is` |
| Documented error names as constants | the `ErrorName*` block in `errors.go` |
| Idempotency key lifted to a header | `sendSpec` in `emails.go` |
| Request id attached to errors | `apiErrorFrom` reading `X-Request-Id` |
| Timeout | `http.Client.Timeout`, set by `New` |
| Version from build metadata, resolved once | `Version()` over `sync.OnceValue(readVersion)` |
| Concurrency safety, proven not asserted | `concurrency_test.go`, plus `-race` on every CI run |
| Follow the server's next link, never a counter | `nextPageSpec` in `scheduled_emails.go` |
| Path segments escaped | `itemPath` in `serialization.go` |
| Webhook signature verification | `webhooks.go` (no client instance needed) |
| Endpoint-registration handshake | `WebhookHandler.handshake` in `webhooks_http.go` |
| The catalogue is derived, not hand-listed | `eventTypes` plus `EventTypes()` in `events.go` |
| Secrets redacted from logs | `redactHeaders` in `logging.go` |

## Tests

`client_test.go`, `config_test.go`, `scheduled_emails_test.go`, `events_test.go`,
`webhooks_test.go`, `webhooks_http_test.go` and `concurrency_test.go` are **external**
(`package ..._test`), so they exercise only the public API, which is the best guard against
accidentally widening it. `internal_test.go` is the one internal test file, covering the
unexported seams (URL building, the category table, the wire helpers, the logging plumbing)
that cannot be reached from outside.

Two guards worth keeping intact when you add an event:

- `TestTheCatalogueTheFixturesAndTheChecksAllAgree` matches `EventTypes()` against the fixture
  map and the assertion map in both directions, so a type registered without a fixture fails.
- Each fixture asserts its **distinctive nested field**, which is what catches a mis-keyed
  registration: registering `&SentData{}` under `email.delivered` type-checks, and any weaker
  assertion would pass while the struct came back empty.

Coverage is **statement only**. Go's tooling reports statement coverage and has no reliable
branch metric, so there is nothing further to gate on.

Examples carry `//go:build ignore`, which keeps them out of `go build ./...`, `go vet` and the
coverage denominator. The `test` CI job compiles each one explicitly
(`go build -o /dev/null examples/<file>.go`) so an API change cannot silently rot them.
