# ailoop

A workflow engine for LLM-assisted development. It implements the **AI Loop** protocol: a
work item advances through phases, every artifact is approved explicitly, and **the close
is granted by the project, not by the model**.

It works with any provider — Claude, Gemini, any OpenAI-compatible endpoint, a local
Ollama — without changing the flow.

## The idea in one line

> The LLM proposes; the workflow validates and authorizes.

An agent that finishes its turn does not thereby have permission to advance. Every
transition has guards, and `DONE` is granted only after running the commands the project
itself declared.

## Usage

```sh
ailoop start "description of the task"   # creates the context and detects the stack
ailoop next [--files a.go,b.go]          # runs the current phase's agent
ailoop next --inline                     # ...pasting those files in full into the prompt
ailoop next --critic                     # ...with adversarial review
ailoop golden ["note"]                   # runs the golden cases and records the run
ailoop status                            # current phase and approval state
ailoop history                           # archived revisions of every artifact
ailoop decide "what" "why"               # records an approved decision
ailoop decisions                         # lists the Decision Record
ailoop verify                            # runs the project's own checks
ailoop capabilities                      # what the model can do, and what each phase allows
ailoop mcp [--schemas]                   # tools from the MCP servers
ailoop cost                              # token spend per phase
ailoop undo                              # goes back and reverts the patches
```

### Model configuration

Chosen by environment variable, in this order:

| Variable | Effect |
|---|---|
| `ANTHROPIC_API_KEY` | use Claude (`ANTHROPIC_MODEL`, default `claude-opus-5`) |
| `GEMINI_API_KEY` | use Gemini (`GEMINI_MODEL`, default `gemini-3.5-flash`) |
| `OPENAI_BASE_URL` | OpenAI-compatible endpoint (default `http://localhost:11434/v1`, Ollama) |
| `OPENAI_API_KEY`, `OPENAI_MODEL` | credential and model for that endpoint |

A project can also route **phases to providers**, so the judgment-heavy phases run on a
strong model while the mechanical ones stay cheap — and so the verifier runs on
**different weights** than the implementer, which is what decorrelates their blind spots.

## The phases

```
DISCOVERY -> DESIGN -> PLAN -> IMPLEMENTATION -> VERIFICATION -> DONE
```

Going back with `undo` does not destroy what was written: the revision is archived and can
be recovered with `ailoop history`.

## The three levels of control

The engine's value is not in its prompts but in what does **not** depend on the model
behaving well.

| Level | Mechanism | Where it lives |
|---|---|---|
| **Deterministic** | the project's own commands decide whether it closes | `internal/verify` |
| **Structural** | transition guards, patch parsing, path validation, citation checking | `internal/state/guards.go`, `internal/patch`, `internal/agents` |
| **Semantic** | adversarial critic agent | `internal/agents/critic.go` |

An agent cannot grant itself an exception to either of the first two.

## Verification: what "done" means

`ailoop start` writes `.ailoop/config.json` with the commands that define "verified" for
this project:

```json
{
  "verify": [
    {"name": "build", "cmd": "go build ./..."},
    {"name": "test",  "cmd": "go test ./... -count=1"}
  ],
  "timeout_seconds": 300
}
```

If the stack is not recognized, the list stays **empty** and the loop cannot reach `DONE`
until you fill it in. An empty list is never read as "everything is fine": nothing having
run is not everything having passed.

## The agent's tools

The agent can look at the real project instead of imagining it:

```
<<TOOL>>
fs.grep: func Normalize -- **/*.go
<<END>>
```

| Capability | What it does |
|---|---|
| `fs.read` | read a file from the workspace |
| `fs.list` | list a directory |
| `fs.glob` | find files by pattern (`**` crosses directories) |
| `fs.grep` | search contents by regular expression, with an optional `-- <glob>` |
| `fs.read_image` | attach an image, **only if the model can see** |
| `mcp.describe` / `mcp.call` | tools from external servers |
| `cmd.run` | run a declared check, **only in VERIFICATION** |

The protocol is plain text on purpose: native function calling varies between providers,
and some local models do not have it at all.

**Capability, permission and authority are three different things.** A capability needs two
gates: the phase must allow it **and** the model must actually have it. `ailoop
capabilities` shows both.

Only the verifier may run the checks: an implementer able to run and fix its own tests is
its own verifier.

## MCP servers

Any Model Context Protocol server becomes capabilities of the loop, without reimplementing
it here:

```json
"mcp": [
  {"name": "codegraph", "command": "cgc-nix", "args": ["mcp", "start"]}
]
```

Optionally `"tools": ["find_code", "analyze_code_relationships"]` per server, to expose
only what you use.

**Tools are loaded lazily.** The prompt carries a one-line catalog per tool; the agent asks
for `mcp.describe` only for the one it is about to use. Measured against CodeGraph (25
tools): **465 tokens against 1,660 — 72% less on every request**. `ailoop mcp` shows the
comparison.

A server that fails to start is reported and its tools are **not offered**: planning around
a dead tool is worse than not having it.

## Token spend

`ailoop cost` gives the breakdown per phase, with cache hit rate. All three adapters read
the real usage their API reports; when a local server reports nothing, it is estimated and
**marked as estimated** — and that mark is sticky: a total that mixes measured with
estimated is an estimate.

Four measures, all provider-agnostic except where noted:

| Measure | Effect |
|---|---|
| **Stable prefix** | the system prompt carries nothing volatile. Claude needs the explicit marker, OpenAI endpoints do it on their own; the correct shape of the prompt is the same either way |
| **Lazy MCP catalog** | −72% of the cost of tool definitions |
| **`--files` by reference** | names the files; the agent reads the ones it needs. `--inline` keeps the full dump |
| **Observation masking** | elides old observations from the tool loop. Measured: **−52% at 6 rounds, −68% at 10** |

Masking **does not engage** below 12k tokens: rewriting old messages invalidates the cached
prefix, and that would cost more than it saves in a short conversation. It never elides an
observation containing an error — hiding it breaks the very cycle that is diagnosing it.

## Research on the internet

An agent that writes code and also browses can put your code into a URL. So it is **not the
same agent**:

| | Main agent | Research agent |
|---|---|---|
| Workspace, code, decisions | ✅ | ❌ |
| `fs.read`, `fs.grep`, `fs.glob` | ✅ | ❌ |
| **Network** | ❌ **in every phase** | ✅ |
| Receives | the full task | **only a question, 500 characters maximum** |

The main agent delegates with `research.ask`; it never touches the network itself. The
researcher cannot leak what it never had — a structural property, not a bet on the model's
behavior.

**It searches anywhere.** There is no destination allowlist: restricting destinations does
not defend the real channel — what leaves is the question — and it would make research
useless, because you do not know in advance where the answer is.

**Except the local network**, which is always blocked and is not configurable:
`localhost`, private IPs, link-local and `.internal`. The researcher holds nothing from the
project, but it runs inside your perimeter; reaching your router or a cloud metadata
endpoint is not exfiltration, it is SSRF. Redirects are re-checked, for the same reason.

Everything it brings back arrives wrapped in `UNTRUSTED_CONTENT` with the rule stated next
to the content: it is third-party data, never instructions. Prompt injection is the
expected case.

```json
"web": {"enabled": true}
```

`enabled` is explicit, and `ailoop start` writes it into the config so you can see it and
turn it off. Optionally `"allowed"` restricts destinations and `"blocked"` excludes them.

**What stays open:** the question is written by the main agent, which does see the code.
That channel cannot be closed without making the feature useless; it is kept **narrow**
(500 characters), **visible** (shown like any other tool) and **logged**.

## References in text

Anything you write — the task, the reason for a rejection — accepts references:

```
ailoop start "fix the alignment, look at @screenshot.png and @src/button.tsx"
```

| Reference | What it attaches | Requires |
|---|---|---|
| `@main.go` | the file's contents | — |
| `@~/Downloads/spec.pdf` | the PDF's text | `pdftotext` |
| `@screenshot.png` | the image | a model with vision |
| `@screen` / `@screen:select` | the screen, or a region | `grim` / `slurp` |
| `@clipboard` | whatever you copied, text or image | `wl-paste` |

`ailoop doctor` tells you which of those tools you have and what you lose for the ones you
are missing.

### Two rules that make this safe

**References are yours, not the agent's.** That is why they may leave the workspace:
writing `@~/Downloads/spec.pdf` **is** the authorization, given case by case. The agent,
with `fs.read` and `fs.grep`, stays confined to the workspace — nobody authorized anything
there.

**`@screen` is never a capability of the agent.** It happens only because you typed it. An
agent that could capture the screen whenever it wanted would see your password manager,
your mail, whatever you have open.

### What is volatile gets frozen

If you reject a proposal with *"this is wrong, look at @screen"*, the capture is taken **at
that moment** and stored alongside the work item's state. The agent reads that reason on
the next run, when the screen already shows something else; without freezing it, it would
photograph whatever happened to be there.

## Portability

The core is Go and depends on nothing: the state machine, the guards, the ledger, the
budget and the patching behave the same everywhere. What varies is the environment, and
those capabilities are **discovered**, not assumed.

```sh
nix develop    # develop: the tools on the PATH
nix build      # the binary, with its dependencies attached
nix run github:cRolandoJr/ailoop
```

The difference matters: a devShell resolves the PATH of whoever opens it; the package uses
`wrapProgram`, so the binary carries `poppler-utils`, `grim`, `slurp` and `wl-clipboard`
without them being installed on the machine.

It also works without Nix: capabilities whose tool is missing are simply not offered, and
`ailoop doctor` says which ones.

## Patch safety

- Paths are validated against the workspace: absolute ones, and ones escaping with `..`,
  are rejected.
- If **any** block of the patch is invalid, **none** of it is applied.
- An ambiguous search block (several matches) is rejected instead of patching "the first
  one".
- Every file is backed up preserving its path before being touched, and `undo` really
  restores, reporting what it restored and what it could not.
- `cmd.run` executes **only** the commands declared in the config: there is no arbitrary
  shell on any path.

## Architecture

```
main.go                 CLI and orchestration
internal/state          phases, guards, history, Decision Record, spend ledger, turn journal
internal/verify         runs the project's commands
internal/config         what "verified" means here, and which MCP servers exist
internal/golden         golden cases: declarative checks over what the circuit produces
internal/agents         per-phase agents, critic, permissions, tool loop, masking, citations
internal/tools          capabilities and their bounded execution
internal/mcp            Model Context Protocol client
internal/patch          patches with backup and rollback
internal/llm            LLM port + adapters (Claude, Gemini, OpenAI)
internal/ui             human approval
```

`internal/llm.Client` is a two-method port: adding a new provider touches nothing else.

## The same protocol on Claude Code

The protocol also ships as a Claude Code plugin, where Claude Code itself is the host and
the skill carries only the protocol:
[`cRolandoJr/ai-loop-skill`](https://github.com/cRolandoJr/ai-loop-skill).

## Status

A working MVP, with tests. Outstanding:

- the Claude adapter is tested in its logic but has not yet called the real API;
- the OpenAI-compatible adapter does not send images (which is why it declares
  `Vision: unknown` — fail-closed, so they are never offered to it);
- Gemini has explicit context caching that this adapter does not use.

## License

MIT — see [LICENSE](LICENSE).
