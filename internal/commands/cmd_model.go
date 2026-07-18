package commands

// cmd_model.go — /model command and subcommands (ls, set, direct).

import (
	"context"
	"fmt"
	"strings"

	"github.com/DojoGenesis/cli/internal/activity"
	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/providers"
	gcolor "github.com/gookit/color"
)

// ─── /model ─────────────────────────────────────────────────────────────────

func (r *Registry) modelCmd() Command {
	return Command{
		Name:    "model",
		Aliases: []string{"models"},
		Usage:   "/model [ls|set <provider> <model>|direct <provider> <model> <msg>]",
		Short:   "List models/providers, switch model, or send a direct API call",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 || strings.ToLower(args[0]) == "ls" {
				return r.modelList(ctx)
			}

			sub := strings.ToLower(args[0])

			// /model set [<provider>] <model>
			if sub == "set" {
				return r.modelSet(ctx, args[1:])
			}

			// /model direct <provider> <model> <message...>
			if sub == "direct" {
				return r.modelDirect(ctx, args[1:])
			}

			return fmt.Errorf("unknown /model subcommand %q — use ls, set, or direct", sub)
		},
	}
}

// ─── JSON result types (headless mode) ──────────────────────────────────────

// ModelCatalogModel is one statically-known model within a provider, in the
// JSON-mode `/model ls` catalog. providers.KnownModel carries no JSON tags
// (it's a REPL-only display type), so this is the small tagged projection.
type ModelCatalogModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Notes       string `json:"notes,omitempty"`
}

// ModelCatalogEntry is one provider's row in the JSON-mode `/model ls`
// catalog — the structured equivalent of providers.FormatProviderTable's
// human table, including the same "n/a" / "set" / "not set" key status.
type ModelCatalogEntry struct {
	ID          string              `json:"id"`
	DisplayName string              `json:"display_name"`
	KeyStatus   string              `json:"key_status"` // "n/a" | "set" | "not set"
	Models      []ModelCatalogModel `json:"models"`
}

// ModelListResult is the JSON-mode payload for `/model ls` (and bare
// `/model`). GatewayProviders/GatewayModels are mutually exclusive — the
// gateway's /v1/providers is tried first, /v1/models is a fallback, and
// both are empty when the gateway is unreachable.
type ModelListResult struct {
	Catalog          []ModelCatalogEntry `json:"catalog"`
	GatewayReachable bool                `json:"gateway_reachable"`
	GatewayProviders []client.Provider   `json:"gateway_providers,omitempty"`
	GatewayModels    []client.Model      `json:"gateway_models,omitempty"`
	DelegationModel  string              `json:"delegation_model,omitempty"`
}

// modelCatalogEntries builds the JSON-mode catalog rows from providers.Catalog,
// mirroring providers.FormatProviderTable's key-status logic exactly.
func modelCatalogEntries(keys providers.APIKeys) []ModelCatalogEntry {
	entries := make([]ModelCatalogEntry, 0, len(providers.Catalog))
	for _, p := range providers.Catalog {
		keyStatus := "n/a"
		if p.EnvKey != "" {
			if keys.KeyForProvider(p.ID) != "" {
				keyStatus = "set"
			} else {
				keyStatus = "not set"
			}
		}
		models := make([]ModelCatalogModel, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, ModelCatalogModel{ID: m.ID, DisplayName: m.DisplayName, Notes: m.Notes})
		}
		entries = append(entries, ModelCatalogEntry{
			ID:          p.ID,
			DisplayName: p.DisplayName,
			KeyStatus:   keyStatus,
			Models:      models,
		})
	}
	return entries
}

func (r *Registry) modelList(ctx context.Context) error {
	// Always show static provider catalog first.
	keys := providers.LoadAPIKeys()
	catalog := providers.FormatProviderTable(keys)

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprint("  Provider catalog (direct API keys)"))
	fmt.Println()
	fmt.Println()
	for _, line := range strings.Split(strings.TrimRight(catalog, "\n"), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()

	// Also show gateway-discovered providers/models (best-effort).
	gwProviders, err := r.gw.Providers(ctx)
	if err != nil {
		// Fallback: try /v1/models.
		gwModels, err2 := r.gw.Models(ctx)
		if err2 != nil {
			// Gateway is offline — that's fine, catalog was already shown.
			if r.out.JSON() {
				r.out.Data(ModelListResult{
					Catalog:         modelCatalogEntries(keys),
					DelegationModel: r.cfg.Delegation.Model,
				})
				return nil
			}
			fmt.Println(gcolor.HEX("#94a3b8").Sprint("  (gateway unreachable — showing catalog only)"))
			fmt.Println()
			r.printDelegationDefault()
			return nil
		}
		if r.out.JSON() {
			r.out.Data(ModelListResult{
				Catalog:          modelCatalogEntries(keys),
				GatewayReachable: true,
				GatewayModels:    gwModels,
				DelegationModel:  r.cfg.Delegation.Model,
			})
			return nil
		}
		gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Gateway models (%d)", len(gwModels)))
		fmt.Println()
		fmt.Println()
		for _, m := range gwModels {
			fmt.Printf("  %s  %s\n",
				gcolor.HEX("#f4a261").Sprintf("%-42s", m.ID),
				gcolor.HEX("#94a3b8").Sprint(m.Provider),
			)
		}
		fmt.Println()
		r.printDelegationDefault()
		return nil
	}

	if r.out.JSON() {
		r.out.Data(ModelListResult{
			Catalog:          modelCatalogEntries(keys),
			GatewayReachable: true,
			GatewayProviders: gwProviders,
			DelegationModel:  r.cfg.Delegation.Model,
		})
		return nil
	}

	gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Gateway providers (%d)", len(gwProviders)))
	fmt.Println()
	fmt.Println()
	for _, p := range gwProviders {
		status := colorStatus(p.Status)
		caps := ""
		if p.Info != nil && len(p.Info.Capabilities) > 0 {
			caps = gcolor.HEX("#94a3b8").Sprintf(" [%s]", strings.Join(p.Info.Capabilities, ", "))
		}
		fmt.Printf("  %s  %s%s\n", gcolor.HEX("#f4a261").Sprintf("%-20s", p.Name), status, caps)
	}
	fmt.Println()
	r.printDelegationDefault()
	return nil
}

// printDelegationDefault appends one line to /model ls output naming the
// /agent dispatch delegation-model default (cfg.Delegation.Model), when set.
// This is local config state — independent of gateway connectivity — so
// modelList calls it from all three of its exit paths (catalog-only,
// gwModels fallback, and gwProviders), and it is a silent no-op when the
// delegation default is unset ("").
func (r *Registry) printDelegationDefault() {
	if r.cfg.Delegation.Model == "" {
		return
	}
	fmt.Printf("  %s\n", gcolor.HEX("#94a3b8").Sprintf(
		"delegation default: %s (used by /agent dispatch unless --model is given)",
		r.cfg.Delegation.Model,
	))
	fmt.Println()
}

func (r *Registry) modelSet(ctx context.Context, args []string) error {
	// /model set <model>  OR  /model set <provider> <model>
	var newProvider, newModel, oldModel string
	oldModel = r.cfg.Defaults.Model
	if oldModel == "" {
		oldModel = "(auto)"
	}

	switch len(args) {
	case 0:
		return fmt.Errorf("usage: /model set [<provider>] <model>")
	case 1:
		newModel = args[0]
	default:
		newProvider = args[0]
		newModel = args[1]
	}

	r.cfg.Defaults.Model = newModel
	if newProvider != "" {
		r.cfg.Defaults.Provider = newProvider
	} else {
		// Auto-infer provider from model name when not explicitly given
		if inferred := providers.InferProvider(newModel); inferred != "" {
			r.cfg.Defaults.Provider = inferred
			newProvider = inferred // used for display and key sync below
		}
	}
	_ = r.cfg.Save() // persist model/provider to disk

	activity.Log(activity.ModelChanged, fmt.Sprintf("model → %s", newModel))

	// If a provider was specified (or inferred), push its API key to the gateway so it gets
	// registered immediately rather than waiting for the next restart.
	if newProvider != "" {
		keys := providers.LoadAPIKeys()
		if key := keys.KeyForProvider(newProvider); key != "" {
			if err := r.gw.SetProviderKey(ctx, newProvider, key); err != nil {
				// Non-fatal: the gateway may be offline or not yet accepting keys.
				fmt.Printf("  %s\n", gcolor.HEX("#94a3b8").Sprintf("(gateway key sync: %v)", err))
			}
		}
	}

	fmt.Println()
	gcolor.Bold.Print(gcolor.HEX("#7fb88c").Sprint("  Model updated"))
	fmt.Println()
	printKV("old model", oldModel)
	printKV("new model", newModel)
	if newProvider != "" {
		printKV("provider", newProvider)
	}
	fmt.Println()
	return nil
}

func (r *Registry) modelDirect(ctx context.Context, args []string) error {
	// /model direct <provider> <model> <message...>
	if len(args) < 3 {
		return fmt.Errorf("usage: /model direct <provider> <model> <message>")
	}
	providerID := args[0]
	modelID := args[1]
	message := strings.Join(args[2:], " ")

	keys := providers.LoadAPIKeys()
	apiKey := keys.KeyForProvider(strings.ToLower(providerID))
	if apiKey == "" {
		return fmt.Errorf("no API key found for %s — set the environment variable and restart", providerID)
	}

	fmt.Println()
	fmt.Printf("  %s %s/%s\n",
		gcolor.HEX("#94a3b8").Sprint("→ direct call to"),
		gcolor.HEX("#f4a261").Sprint(providerID),
		gcolor.HEX("#e8b04a").Sprint(modelID),
	)
	fmt.Println()

	resp, err := providers.Chat(ctx, providers.DirectChatRequest{
		Provider: providerID,
		Model:    modelID,
		Messages: []providers.DirectMessage{
			{Role: "user", Content: message},
		},
		APIKey: apiKey,
	})
	if err != nil {
		return fmt.Errorf("direct call failed: %w", err)
	}

	fmt.Println(resp.Content)
	fmt.Println()
	printKV("model", resp.Model)
	printKV("tokens in", fmt.Sprintf("%d", resp.Usage.InputTokens))
	printKV("tokens out", fmt.Sprintf("%d", resp.Usage.OutputTokens))
	fmt.Println()

	activity.Log(activity.ModelChanged, fmt.Sprintf("direct call: %s/%s (%d out tokens)", providerID, modelID, resp.Usage.OutputTokens))
	return nil
}
