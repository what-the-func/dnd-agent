package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/kronk"
	"github.com/ardanlabs/kronk/sdk/kronk/model"
	"github.com/joho/godotenv"
)

const modelURL = "Qwen/Qwen3-8B-GGUF/Qwen3-8B-Q5_K_M.gguf"

const systemPrompt = `You are a D&D 5e Dungeon Master. The player is a level 5 wizard (32 HP, AC 12) with Fireball, Shield, Misty Step, and Magic Missile prepared. They stand at a dungeon entrance.

Keep responses to 2-3 sentences max. Never ramble. After describing the scene, stop and use ask_player immediately.`

var playerScanner = bufio.NewScanner(os.Stdin)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	godotenv.Load()

	provider, err := kronk.New(
		kronk.WithName("kronk"),
		kronk.WithLogger(kronk.FmtLogger),
		kronk.WithModelConfig(model.Config{
			CacheTypeK: model.GGMLTypeQ8_0,
			CacheTypeV: model.GGMLTypeQ8_0,
			NBatch:     512,
		}),
	)
	if err != nil {
		return fmt.Errorf("provider: %w", err)
	}
	defer func() {
		if c, ok := provider.(interface{ Close(context.Context) error }); ok {
			c.Close(context.Background())
		}
	}()

	llm, err := provider.LanguageModel(sigCtx, modelURL)
	if err != nil {
		return fmt.Errorf("model: %w", err)
	}

	agent := fantasy.NewAgent(llm,
		fantasy.WithSystemPrompt(systemPrompt),
		fantasy.WithTools(
			playerTool(),
			monsterTool(),
			spellTool(),
			diceTool(),
		),
		fantasy.WithMaxOutputTokens(2048),
		fantasy.WithTemperature(0.8),
	)

	return gameLoop(sigCtx, agent)
}

// ---------------------------------------------------------------------------
// Game loop
// ---------------------------------------------------------------------------

// gameLoop runs the turn-based game indefinitely until Ctrl+C.
// Each iteration is one DM turn. Conversation history accumulates between turns.
func gameLoop(sigCtx context.Context, agent fantasy.Agent) error {
	var history []fantasy.Message
	prompt := "Begin."

	fmt.Println("=== D&D 5e ===")
	fmt.Println("Press Ctrl+C to quit")
	fmt.Println()

	for {
		ctx, cancel := context.WithTimeout(sigCtx, 30*time.Minute)

		result, err := agent.Stream(ctx, fantasy.AgentStreamCall{
			Prompt:           prompt,
			Messages:         history,
			OnReasoningStart: onReasoningStart,
			OnReasoningDelta: onReasoningDelta,
			OnReasoningEnd:   onReasoningEnd,
			OnTextDelta:      onTextDelta,
			OnToolCall:       onToolCall,
			OnToolResult:     onToolResult,
		})
		cancel()

		if err != nil {
			if sigCtx.Err() != nil {
				fmt.Println("\n\n--- Thanks for playing! ---")
				return nil
			}
			return fmt.Errorf("stream: %w", err)
		}

		for _, step := range result.Steps {
			history = append(history, step.Messages...)
		}

		fmt.Println()
		prompt = "Continue."
	}
}

// ---------------------------------------------------------------------------
// Stream callbacks
// ---------------------------------------------------------------------------

func onReasoningStart(_ string, _ fantasy.ReasoningContent) error {
	fmt.Println("\n[THINKING...]")
	return nil
}

func onReasoningDelta(_, text string) error {
	fmt.Print(text)
	return nil
}

func onReasoningEnd(_ string, _ fantasy.ReasoningContent) error {
	fmt.Println("\n[END THINKING]\n")
	return nil
}

func onTextDelta(_, text string) error {
	fmt.Print(text)
	return nil
}

func onToolCall(tc fantasy.ToolCallContent) error {
	if tc.ToolName != "ask_player" {
		fmt.Printf("\n[%s] %s\n", tc.ToolName, tc.Input)
	}
	return nil
}

func onToolResult(res fantasy.ToolResultContent) error {
	if res.ToolName != "ask_player" {
		fmt.Println("-> done")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tool definitions
// ---------------------------------------------------------------------------

func playerTool() fantasy.AgentTool {
	return fantasy.NewAgentTool("ask_player",
		"Present the player with choices. You must call this whenever it is the "+
			"player's turn to act. Provide a question and 3-5 options. Do not write "+
			"options in your response text — this tool handles the display. The game "+
			"cannot continue until the player chooses.",
		askPlayer,
	)
}

func monsterTool() fantasy.AgentTool {
	return fantasy.NewAgentTool("lookup_monster",
		"Look up a D&D 5e monster by name to get its real stats. Always call "+
			"this before using any monster in the game.",
		lookupMonster,
	)
}

func spellTool() fantasy.AgentTool {
	return fantasy.NewAgentTool("lookup_spell",
		"Look up a D&D 5e spell by name to get its real details. Always call "+
			"this before resolving a spell.",
		lookupSpell,
	)
}

func diceTool() fantasy.AgentTool {
	return fantasy.NewAgentTool("roll_dice",
		"Roll dice. Specify the number of dice, sides per die, and an optional "+
			"modifier. Always call this — never generate random numbers yourself.",
		rollDice,
	)
}

// ---------------------------------------------------------------------------
// Tool: ask_player
// ---------------------------------------------------------------------------

type askPlayerInput struct {
	Question string   `json:"question" description:"The question to ask the player"`
	Options  []string `json:"options" description:"List of 3-5 options the player can choose from"`
}

func askPlayer(_ context.Context, input askPlayerInput, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	fmt.Printf("\n\n--- YOUR TURN ---\n%s\n\n", input.Question)
	for i, opt := range input.Options {
		fmt.Printf("  %d. %s\n", i+1, opt)
	}

	for {
		fmt.Printf("\nChoose [1-%d]: ", len(input.Options))

		if !playerScanner.Scan() {
			return fantasy.NewTextResponse("The player has left the game."), nil
		}

		text := strings.TrimSpace(playerScanner.Text())
		choice, err := strconv.Atoi(text)
		if err != nil || choice < 1 || choice > len(input.Options) {
			fmt.Printf("Pick a number between 1 and %d.\n", len(input.Options))
			continue
		}

		chosen := input.Options[choice-1]
		fmt.Printf("-> %s\n\n", chosen)
		return fantasy.NewTextResponse(fmt.Sprintf("The player chose: %s", chosen)), nil
	}
}

// ---------------------------------------------------------------------------
// Tool: lookup_monster
// ---------------------------------------------------------------------------

type monsterQuery struct {
	Name string `json:"name" description:"Monster name, e.g. owlbear, dragon, goblin"`
}

func lookupMonster(_ context.Context, input monsterQuery, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	slug := strings.ToLower(strings.ReplaceAll(input.Name, " ", "-"))

	resp, err := http.Get("https://www.dnd5eapi.co/api/monsters/" + slug)
	if err != nil {
		return fantasy.NewTextResponse("Failed to reach D&D API"), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fantasy.NewTextResponse(fmt.Sprintf("Monster '%s' not found", input.Name)), nil
	}

	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	json.Unmarshal(body, &m)

	summary := fmt.Sprintf(
		"%s (%s %s, CR %v) | AC %v | HP %v (%v)\n"+
			"STR %v DEX %v CON %v INT %v WIS %v CHA %v | Speed: %v",
		m["name"], m["size"], m["type"], m["challenge_rating"],
		formatAC(m["armor_class"]), m["hit_points"], m["hit_dice"],
		m["strength"], m["dexterity"], m["constitution"],
		m["intelligence"], m["wisdom"], m["charisma"],
		formatSpeed(m["speed"]),
	)

	if actions, ok := m["actions"].([]any); ok {
		summary += "\nActions:"
		for _, a := range actions {
			act := a.(map[string]any)
			summary += fmt.Sprintf("\n- %s: %s", act["name"], act["desc"])
		}
	}

	if abilities, ok := m["special_abilities"].([]any); ok {
		summary += "\nSpecial Abilities:"
		for _, a := range abilities {
			ab := a.(map[string]any)
			summary += fmt.Sprintf("\n- %s: %s", ab["name"], ab["desc"])
		}
	}

	return fantasy.NewTextResponse(summary), nil
}

// ---------------------------------------------------------------------------
// Tool: lookup_spell
// ---------------------------------------------------------------------------

type spellQuery struct {
	Name string `json:"name" description:"Spell name, e.g. fireball, magic-missile, shield"`
}

func lookupSpell(_ context.Context, input spellQuery, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	slug := strings.ToLower(strings.ReplaceAll(input.Name, " ", "-"))

	resp, err := http.Get("https://www.dnd5eapi.co/api/spells/" + slug)
	if err != nil {
		return fantasy.NewTextResponse("Failed to reach D&D API"), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fantasy.NewTextResponse(fmt.Sprintf("Spell '%s' not found", input.Name)), nil
	}

	body, _ := io.ReadAll(resp.Body)
	var s map[string]any
	json.Unmarshal(body, &s)

	desc := ""
	if descs, ok := s["desc"].([]any); ok && len(descs) > 0 {
		desc = fmt.Sprintf("%v", descs[0])
	}

	summary := fmt.Sprintf(
		"%s (Level %v %s) | %s | Range: %s | Duration: %s\nComponents: %v\n%s",
		s["name"], s["level"], formatSchool(s["school"]),
		s["casting_time"], s["range"], s["duration"],
		s["components"], desc,
	)

	if dmg, ok := s["damage"].(map[string]any); ok {
		if atSlot, ok := dmg["damage_at_slot_level"].(map[string]any); ok {
			summary += "\nDamage by slot:"
			for lvl, dice := range atSlot {
				summary += fmt.Sprintf(" L%s=%v", lvl, dice)
			}
		}
	}

	return fantasy.NewTextResponse(summary), nil
}

// ---------------------------------------------------------------------------
// Tool: roll_dice
// ---------------------------------------------------------------------------

type diceQuery struct {
	Count    int `json:"count" description:"Number of dice to roll (e.g. 2 for 2d6)"`
	Sides    int `json:"sides" description:"Sides per die (e.g. 20 for d20)"`
	Modifier int `json:"modifier" description:"Added to total (e.g. 5 for +5, -2 for penalty). Default 0."`
}

func rollDice(_ context.Context, input diceQuery, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
	count := max(input.Count, 1)
	sides := max(input.Sides, 1)

	rolls := make([]int, count)
	total := 0
	for i := range count {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(sides)))
		rolls[i] = int(n.Int64()) + 1
		total += rolls[i]
	}
	total += input.Modifier

	notation := fmt.Sprintf("%dd%d", count, sides)
	if input.Modifier != 0 {
		notation += fmt.Sprintf("%+d", input.Modifier)
	}

	return fantasy.NewTextResponse(fmt.Sprintf("Rolling %s: %v = %d", notation, rolls, total)), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func formatAC(ac any) string {
	if arr, ok := ac.([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			return fmt.Sprintf("%v", m["value"])
		}
	}
	return "?"
}

func formatSpeed(speed any) string {
	m, ok := speed.(map[string]any)
	if !ok {
		return "?"
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s %v", k, v))
	}
	return strings.Join(parts, ", ")
}

func formatSchool(school any) string {
	if m, ok := school.(map[string]any); ok {
		return fmt.Sprintf("%v", m["name"])
	}
	return "?"
}
