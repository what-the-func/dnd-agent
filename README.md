# 🐉 dnd-agent

An AI Dungeon Master built in Go using [Fantasy](https://github.com/charmbracelet/fantasy), [Kronk](https://github.com/ardanlabs/kronk), and [yzma](https://github.com/hybridgroup/yzma). Runs entirely on your local machine — no API keys, no cloud, no monthly bill.

> 📺 Watch the tutorial: [YouTube link coming soon]

## What It Does

This agent acts as a D&D 5th Edition Dungeon Master with three tools:

- **🐻 Monster Lookup** — Fetches real monster stats from the [D&D 5e API](https://www.dnd5eapi.co/)
- **✨ Spell Lookup** — Pulls actual spell details (damage, range, components, descriptions)
- **🎲 Dice Roller** — Rolls any standard dice notation (`2d6`, `1d20+5`, `8d6`)

The DM uses these tools to run mechanically accurate encounters with dramatic narration — powered by a local LLM running on your hardware.

## The Stack

```
┌─────────────────────────────────┐
│  dnd-agent (Fantasy API)        │  ← Your code
├─────────────────────────────────┤
│  Kronk Provider                 │  ← Local inference SDK
├─────────────────────────────────┤
│  yzma (llama.cpp bindings)      │  ← Pure Go FFI (no CGo!)
├─────────────────────────────────┤
│  llama.cpp + GPU acceleration   │  ← CUDA / Metal / Vulkan
└─────────────────────────────────┘
```

## Prerequisites

- **Go 1.22+**
- **16GB RAM** (for Qwen3-8B model)
- Optional: NVIDIA GPU (CUDA), Apple Silicon (Metal), or Vulkan-capable GPU

## Quick Start

```bash
git clone https://github.com/what-the-func/dnd-agent.git
cd dnd-agent
cp .env.example .env
go run .
```

On first run, Kronk automatically downloads:
1. The llama.cpp libraries for your platform
2. The Qwen3-8B model (~8GB)

Subsequent runs start immediately from cache.

## GPU Configuration

Copy `.env.example` to `.env` and set `KRONK_PROCESSOR` to match your hardware:

| Value | Backend | Hardware |
|-------|---------|----------|
| `cpu` | CPU only | Any (default) |
| `cuda` | NVIDIA CUDA | NVIDIA GPUs |
| `vulkan` | Vulkan | NVIDIA, AMD, Intel GPUs |
| `metal` | Apple Metal | Apple Silicon Macs |

Example `.env` for an NVIDIA GPU with Vulkan:

```bash
KRONK_PROCESSOR=vulkan
GGML_VK_VISIBLE_DEVICES=0
```

If you have multiple GPUs (e.g. discrete + integrated), set the device index to
target the right one. Run with `DND_DEBUG=1` to list detected devices:

```bash
DND_DEBUG=1 go run .
```

**Important:** After changing `KRONK_PROCESSOR`, delete the cached libraries so
Kronk re-downloads the correct backend build:

```bash
rm -rf ~/.kronk/libraries
```

Environment variables set in the shell always override `.env` values, so you can
do one-off overrides without editing the file:

```bash
KRONK_PROCESSOR=cuda go run .
```

## How It Works

The agent uses Fantasy's multi-provider architecture with Kronk as the local inference backend:

1. **Fantasy** provides the agent framework — system prompts, tool definitions, streaming
2. **Kronk** handles model management and local inference via yzma
3. **yzma** binds directly to llama.cpp using `purego` — no CGo needed
4. The **D&D 5e API** provides real game data (monsters, spells) — no auth required

### Tools

The agent has three tools that the LLM can call during generation:

```go
// Monster lookup — fetches from dnd5eapi.co
monsterTool := fantasy.NewAgentTool("lookup_monster", 
    "Look up a D&D 5e monster by name.",
    lookupMonster,
)

// Spell lookup — fetches from dnd5eapi.co  
spellTool := fantasy.NewAgentTool("lookup_spell",
    "Look up a D&D 5e spell by name.",
    lookupSpell,
)

// Dice roller — parses standard notation
diceTool := fantasy.NewAgentTool("roll_dice",
    "Roll dice using standard notation like 2d6, 1d20+5, or 8d6.",
    rollDice,
)
```

### Streaming with Reasoning

The demo uses Qwen3's "thinking" mode to stream the DM's reasoning process:

```go
streamCall := fantasy.AgentStreamCall{
    Prompt: "Set up an encounter with an owlbear...",
    
    OnReasoningDelta: func(id, text string) error {
        fmt.Print(text) // Watch the DM think
        return nil
    },
    OnTextDelta: func(id, text string) error {
        fmt.Print(text) // Stream the narration
        return nil
    },
    OnToolCall: func(tc fantasy.ToolCallContent) error {
        fmt.Printf("⚔️ [%s]\n", tc.ToolName) // See tool calls
        return nil
    },
}
```

## Swap to Cloud

Since Fantasy is provider-agnostic, you can swap to any cloud provider with one change:

```go
// Local (Kronk)
provider, _ := kronk.New(kronk.WithName("kronk"))

// Cloud (OpenRouter) — same agent code, different provider
provider, _ := openrouter.New(openrouter.WithAPIKey(key))
```

## Model Options

| Model | Size | Notes |
|-------|------|-------|
| Qwen3-8B-Q8_0 (default) | ~8GB | Best balance of quality and speed |
| Qwen2.5-0.5B-Instruct | ~1GB | Fast, lighter tasks |
| SmolLM2-135M | ~135MB | Minimal testing only |

Change the model by updating the `modelURL` constant.

## Links

- [Fantasy](https://github.com/charmbracelet/fantasy) — Multi-provider agent framework
- [Kronk](https://github.com/ardanlabs/kronk) — Local inference SDK
- [yzma](https://github.com/hybridgroup/yzma) — Go llama.cpp bindings
- [D&D 5e API](https://www.dnd5eapi.co/) — Free D&D data
- [Kronk Model Catalog](https://github.com/ardanlabs/kronk_catalogs)

## License

MIT
