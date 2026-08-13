# covey-plugin-template

A skeleton for a **Covey target-system plugin written in Go** and compiled to WebAssembly.

Use this when a JSON manifest is not enough — when your plugin has to paginate, merge two calls into one answer, parse something, decide something. If your system is plain REST with a token in a header and no logic worth the name, write a [manifest](https://github.com/benjaminLedel/covey-plugins) instead; it is simpler and it stays reviewable.

> Use this template → the green **"Use this template"** button, or `gh repo create my-plugin --template benjaminLedel/covey-plugin-template`.

## What you get

```
main.go            your plugin — replace it
covey/plugin.go    the glue: implement an interface, call covey.Run
Makefile           build (reproducibly), lint, digest
.github/workflows  builds on every push and checks the build is reproducible
```

```sh
make build   # -> example.wasm, its size, its sha256 and the Go version
make lint    # the same check Covey runs when somebody installs it
```

## Writing it

Implement two methods; the rest is optional.

```go
func main() { covey.Run(plugin{}) }

func (plugin) Describe() covey.Description { … }                       // what you are
func (plugin) Execute(action string, params json.RawMessage) (any, error) { … }  // the work

func (plugin) Probe() (string, error)                     // optional: connection test
func (plugin) Poll(kind string) (bool, string, error)     // optional: `nur-wenn:` in HEARTBEAT.md
func (plugin) PromptDoc(scopes []string) string           // optional: what the agent reads
```

Declare in `Describe` only what you actually implement. Claiming `Probe: true` without a `Probe` method earns an operator a button that can only fail.

## What your plugin cannot do — and why that is the point

| | |
|---|---|
| Network | none. `covey.Fetch` asks the host; there is no socket in the sandbox. |
| Filesystem | none mounted. |
| The credential | **you never see it.** You give a *path*; Covey adds the base URL and the token an organisation stored. |
| The host | you cannot name one. Absolute URLs are refused. |
| Resources | 64 MiB memory, 60 s per invocation, 64 requests per action, a fresh instance every call. |

So a bug in your plugin cannot leak somebody's token — it never had one. This is what makes it acceptable for a stranger to install your code, and it is worth not fighting: if you find yourself wanting a socket, the plugin probably wants to be a service instead.

## The build is reproducible, on purpose

`make build` uses `-trimpath`. With that, the same source and the same Go version produce **byte-identical** output on any machine — verified, and it matters:

Nobody reviews a 3 MB binary. When you publish, the catalogue's CI checks out your repository at the tag you named, builds it with the Go version you named, and refuses the entry unless the result matches your digest exactly. That is what turns "trust the publisher" into "read the source" — but only if your build is reproducible. Do not remove `-trimpath`, and do not embed timestamps or build hosts.

Go's wasm output carries the whole Go runtime and lands around 3.4 MB. **TinyGo** produces roughly a tenth of that and starts faster; the protocol is plain stdio, so it works just as well. Worth it once your plugin is real.

## Publishing

1. Tag a release here (`v0.1.0`).
2. Attach the `.wasm` to the release, or point at the file at that tag.
3. Open a pull request against [covey-plugins](https://github.com/benjaminLedel/covey-plugins) adding `plugins/<name>.json`:

```json
{
  "name": "example",
  "label": "Example tracker",
  "description": "Find issues, read them, comment.",
  "category": "ticketing",
  "kind": "wasm",
  "publisher": "you",
  "homepage": "https://github.com/you/covey-plugin-example",
  "license": "MIT",
  "versions": [
    {
      "version": "0.1.0",
      "url": "https://github.com/you/covey-plugin-example/releases/download/v0.1.0/example.wasm",
      "sha256": "…",
      "build": {
        "repo": "https://github.com/you/covey-plugin-example",
        "ref": "v0.1.0",
        "go": "1.26.5"
      }
    }
  ]
}
```

The `build` block is required for wasm entries — it is what CI rebuilds against. `make build` prints the digest and the Go version to put here.

## Testing it

`covey plugin lint example.wasm` loads the module and asks it what it is — the same thing an instance does at install time. It also tells you what you are giving up: no probe, no poll, actions without doc lines.

For the behaviour itself, write ordinary Go tests against your logic. The one thing you cannot test locally is the host: keep the code that decides things separate from the code that calls `covey.Fetch`, and that stops mattering.

## Licence

MIT — do what you like with it, including relicensing your plugin.
