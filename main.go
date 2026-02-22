package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/kronk"
)

const modelURL = "Qwen/Qwen3-8B-GGUF/Qwen3-8B-Q8_0.gguf"

const systemPrompt = `You are an experienced D&D 5th Edition Dungeon Master.
You have access to tools that let you look up real monster stats, spell details,
and roll dice. Use them to run vivid, accurate encounters.

When describing combat, always look up the actual monster stats first. When a
player wants to cast a spell, look up the real spell details. Roll dice for
attack rolls, damage, and saving throws — never make up results.

Be dramatic and descriptive, but keep responses concise. Use the actual game
mechanics from your tool results.`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Create the Kronk provider (local inference)
	provider, err := kronk.New(
		kronk.WithName("kronk"),
		kronk.WithLogger(kronk.FmtLogger),
	)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	defer func() {
		if closer, ok := provider.(interface{ Close(context.Context) error }); ok {
			closer.Close(context.Background())
		}
	}()

	// Load the model (auto-downloads on first run)
	model, err := provider.LanguageModel(ctx, modelURL)
	if err != nil {
		return fmt.Errorf("model: %w", err)
	}

	// Build the DM toolkit
	monsterTool := fantasy.NewAgentTool("lookup_monster",
		"Look up a D&D 5e monster by name. Returns stats, abilities, and actions.",
		lookupMonster,
	)

	spellTool := fantasy.NewAgentTool("lookup_spell",
		"Look up a D&D 5e spell by name. Returns level, damage, range, components, and description.",
		lookupSpell,
	)

	diceTool := fantasy.NewAgentTool("roll_dice",
		"Roll dice using standard notation like 2d6, 1d20+5, or 8d6.",
		rollDice,
	)

	// Create the Dungeon Master agent
	agent := fantasy.NewAgent(model,
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(monsterTool, spellTool, diceTool),
		fantasy.WithMaxOutputTokens(2048),
		fantasy.WithTemperature(0.8),
	)

	// Set the scene with streaming
	streamCall := fantasy.AgentStreamCall{
		Prompt: `I'm a level 5 wizard exploring a dark cave. I hear a growl 
		ahead. Set up an encounter with an owlbear. Roll initiative for both 
		of us, and describe what I see. I want to open with Fireball.`,

		OnReasoningStart: func(id string, content fantasy.ReasoningContent) error {
			fmt.Print("\n💭 DM is thinking: ")
			return nil
		},
		OnReasoningDelta: func(id, text string) error {
			fmt.Print(text)
			return nil
		},
		OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
			fmt.Print("\n\n")
			return nil
		},
		OnTextDelta: func(id, text string) error {
			fmt.Print(text)
			return nil
		},
		OnToolCall: func(tc fantasy.ToolCallContent) error {
			fmt.Printf("\n⚔️  [%s] %s\n", tc.ToolName, tc.Input)
			return nil
		},
		OnToolResult: func(res fantasy.ToolResultContent) error {
			fmt.Println("✅ Result received")
			return nil
		},
	}

	result, err := agent.Stream(ctx, streamCall)
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}

	fmt.Printf("\n\n--- Encounter complete. Steps: %d ---\n", len(result.Steps))
	return nil
}

// --- Tool: Monster Lookup ---

type monsterQuery struct {
	Name string `json:"name" description:"Monster name, e.g. owlbear, dragon, goblin"`
}

func lookupMonster(ctx context.Context, input monsterQuery, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	slug := strings.ToLower(strings.ReplaceAll(input.Name, " ", "-"))
	url := fmt.Sprintf("https://www.dnd5eapi.co/api/monsters/%s", slug)

	resp, err := http.Get(url)
	if err != nil {
		return fantasy.NewTextResponse("Failed to reach D&D API"), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fantasy.NewTextResponse(fmt.Sprintf("Monster '%s' not found in the bestiary", input.Name)), nil
	}

	body, _ := io.ReadAll(resp.Body)
	var monster map[string]any
	json.Unmarshal(body, &monster)

	summary := fmt.Sprintf(
		"**%s** (%s %s, CR %v)\n"+
			"AC: %v | HP: %v (%v)\n"+
			"STR %v DEX %v CON %v INT %v WIS %v CHA %v\n"+
			"Speed: %v",
		monster["name"], monster["size"], monster["type"],
		monster["challenge_rating"],
		formatAC(monster["armor_class"]),
		monster["hit_points"], monster["hit_dice"],
		monster["strength"], monster["dexterity"], monster["constitution"],
		monster["intelligence"], monster["wisdom"], monster["charisma"],
		formatSpeed(monster["speed"]),
	)

	if actions, ok := monster["actions"].([]any); ok {
		summary += "\n\nActions:"
		for _, a := range actions {
			action := a.(map[string]any)
			summary += fmt.Sprintf("\n- %s: %s", action["name"], action["desc"])
		}
	}

	if abilities, ok := monster["special_abilities"].([]any); ok {
		summary += "\n\nSpecial Abilities:"
		for _, a := range abilities {
			ability := a.(map[string]any)
			summary += fmt.Sprintf("\n- %s: %s", ability["name"], ability["desc"])
		}
	}

	return fantasy.NewTextResponse(summary), nil
}

// --- Tool: Spell Lookup ---

type spellQuery struct {
	Name string `json:"name" description:"Spell name, e.g. fireball, magic-missile, shield"`
}

func lookupSpell(ctx context.Context, input spellQuery, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	slug := strings.ToLower(strings.ReplaceAll(input.Name, " ", "-"))
	url := fmt.Sprintf("https://www.dnd5eapi.co/api/spells/%s", slug)

	resp, err := http.Get(url)
	if err != nil {
		return fantasy.NewTextResponse("Failed to reach D&D API"), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fantasy.NewTextResponse(fmt.Sprintf("Spell '%s' not found in the spellbook", input.Name)), nil
	}

	body, _ := io.ReadAll(resp.Body)
	var spell map[string]any
	json.Unmarshal(body, &spell)

	desc := ""
	if descs, ok := spell["desc"].([]any); ok && len(descs) > 0 {
		desc = fmt.Sprintf("%v", descs[0])
	}

	summary := fmt.Sprintf(
		"**%s** (Level %v %s)\n"+
			"Casting Time: %s | Range: %s | Duration: %s\n"+
			"Components: %v\n\n%s",
		spell["name"], spell["level"],
		formatSchool(spell["school"]),
		spell["casting_time"], spell["range"], spell["duration"],
		spell["components"],
		desc,
	)

	if dmg, ok := spell["damage"].(map[string]any); ok {
		if atSlot, ok := dmg["damage_at_slot_level"].(map[string]any); ok {
			summary += "\n\nDamage by slot level:"
			for level, dice := range atSlot {
				summary += fmt.Sprintf("\n  Level %s: %v", level, dice)
			}
		}
	}

	return fantasy.NewTextResponse(summary), nil
}

// --- Tool: Dice Roller ---

type diceQuery struct {
	Notation string `json:"notation" description:"Dice notation like 2d6, 1d20+5, 8d6, 1d20-2"`
}

func rollDice(ctx context.Context, input diceQuery, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	notation := strings.TrimSpace(input.Notation)

	var count, sides, modifier int
	modSign := 1

	mainPart := notation
	if idx := strings.IndexAny(notation[1:], "+-"); idx >= 0 {
		idx++
		if notation[idx] == '-' {
			modSign = -1
		}
		fmt.Sscanf(notation[idx+1:], "%d", &modifier)
		modifier *= modSign
		mainPart = notation[:idx]
	}

	parts := strings.SplitN(strings.ToLower(mainPart), "d", 2)
	if len(parts) != 2 {
		return fantasy.NewTextResponse(fmt.Sprintf("Invalid dice notation: %s", notation)), nil
	}

	count, err := strconv.Atoi(parts[0])
	if err != nil || count <= 0 {
		count = 1
	}
	sides, err = strconv.Atoi(parts[1])
	if err != nil || sides <= 0 {
		return fantasy.NewTextResponse(fmt.Sprintf("Invalid dice notation: %s", notation)), nil
	}

	rolls := make([]int, count)
	total := 0
	for i := 0; i < count; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(sides)))
		rolls[i] = int(n.Int64()) + 1
		total += rolls[i]
	}
	total += modifier

	result := fmt.Sprintf("🎲 Rolling %s: %v", notation, rolls)
	if modifier != 0 {
		result += fmt.Sprintf(" %+d", modifier)
	}
	result += fmt.Sprintf(" = **%d**", total)

	return fantasy.NewTextResponse(result), nil
}

// --- Helpers ---

func formatAC(ac any) string {
	if arr, ok := ac.([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			return fmt.Sprintf("%v", m["value"])
		}
	}
	return "?"
}

func formatSpeed(speed any) string {
	if m, ok := speed.(map[string]any); ok {
		parts := []string{}
		for k, v := range m {
			parts = append(parts, fmt.Sprintf("%s %v", k, v))
		}
		return strings.Join(parts, ", ")
	}
	return "?"
}

func formatSchool(school any) string {
	if m, ok := school.(map[string]any); ok {
		return fmt.Sprintf("%v", m["name"])
	}
	return "?"
}
