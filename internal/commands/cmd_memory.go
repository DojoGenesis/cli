package commands

// cmd_memory.go — /trail and /snapshot commands and their helpers.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/DojoGenesis/cli/internal/client"
	gcolor "github.com/gookit/color"
)

// ─── /trail ─────────────────────────────────────────────────────────────────

func (r *Registry) trailCmd() Command {
	return Command{
		Name:  "trail",
		Usage: "/trail [add <text>|edit <id> <text>|rm <id>|search <query>]",
		Short: "Show memory timeline or add/remove/search memories",
		Run: func(ctx context.Context, args []string) error {
			if len(args) == 0 {
				// default: list
				memories, err := r.gw.Memories(ctx)
				if err != nil {
					return fmt.Errorf("could not fetch memory trail: %w", err)
				}
				if r.out.JSON() {
					r.out.Data(memories)
					return nil
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Memory Trail (%d)\n\n", len(memories)))
				if len(memories) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No memory entries yet."))
					fmt.Println()
					return nil
				}
				for _, m := range memories {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#94a3b8").Sprintf("%-20s", m.CreatedAt),
						gcolor.White.Sprint(truncate(m.Content, 80)),
					)
				}
				fmt.Println()
				return nil
			}

			sub := strings.ToLower(args[0])
			switch sub {
			case "add":
				// /trail add <text...>
				if len(args) < 2 {
					return fmt.Errorf("usage: /trail add <text>")
				}
				text := strings.Join(args[1:], " ")
				mem, err := r.gw.StoreMemory(ctx, client.StoreMemoryRequest{Content: text})
				if err != nil {
					return fmt.Errorf("could not store memory: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Memory stored"))
				if mem != nil {
					printKV("id", mem.ID)
				}
				fmt.Println()

			case "edit":
				// /trail edit <id> <text...>
				if len(args) < 3 {
					return fmt.Errorf("usage: /trail edit <id> <text>")
				}
				id := args[1]
				text := strings.Join(args[2:], " ")
				if err := r.gw.UpdateMemory(ctx, id, client.UpdateMemoryRequest{Content: text}); err != nil {
					return fmt.Errorf("could not update memory: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Memory updated"))
				printKV("id", id)
				fmt.Println()

			case "rm":
				// /trail rm <id>
				if len(args) < 2 {
					return fmt.Errorf("usage: /trail rm <id>")
				}
				id := args[1]
				if err := r.gw.DeleteMemory(ctx, id); err != nil {
					return fmt.Errorf("could not delete memory: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Memory deleted"))
				fmt.Println()

			case "search":
				// /trail search <query...>
				if len(args) < 2 {
					return fmt.Errorf("usage: /trail search <query>")
				}
				query := strings.Join(args[1:], " ")
				results, err := r.gw.SearchMemories(ctx, query)
				if err != nil {
					return fmt.Errorf("could not search memories: %w", err)
				}
				if r.out.JSON() {
					r.out.Data(results)
					return nil
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Search results (%d)\n\n", len(results)))
				if len(results) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No results found."))
					fmt.Println()
					return nil
				}
				for _, m := range results {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#94a3b8").Sprintf("%-24s", m.ID),
						gcolor.White.Sprint(truncate(m.Content, 80)),
					)
				}
				fmt.Println()

			default:
				return fmt.Errorf("unknown trail subcommand %q — use: add, edit, rm, search", sub)
			}
			return nil
		},
	}
}

// ─── /snapshot ───────────────────────────────────────────────────────────────

func (r *Registry) snapshotCmd() Command {
	return Command{
		Name:  "snapshot",
		Usage: "/snapshot [save|restore <id>|export <id> [path]|rm <id>]",
		Short: "List, save, restore, export, or delete memory snapshots",
		Run: func(ctx context.Context, args []string) error {
			sub := "ls"
			if len(args) > 0 {
				sub = strings.ToLower(args[0])
			}

			switch sub {
			case "save":
				snap, err := r.gw.CreateSnapshot(ctx, *r.session)
				if err != nil {
					return fmt.Errorf("could not create snapshot: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Snapshot saved"))
				if snap != nil {
					printKV("id", snap.ID)
					printKV("session", snap.SessionID)
					printKV("created", snap.CreatedAt)
				}
				fmt.Println()

			case "restore":
				if len(args) < 2 {
					return fmt.Errorf("usage: /snapshot restore <id>")
				}
				id := args[1]
				if err := r.gw.RestoreSnapshot(ctx, id); err != nil {
					return fmt.Errorf("could not restore snapshot: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Snapshot restored"))
				printKV("id", id)
				fmt.Println()

			case "export":
				if len(args) < 2 {
					return fmt.Errorf("usage: /snapshot export <id> [path]")
				}
				id := args[1]
				data, err := r.gw.ExportSnapshot(ctx, id)
				if err != nil {
					return fmt.Errorf("could not export snapshot: %w", err)
				}
				if len(args) >= 3 {
					// A path was given — write to disk instead of dumping to
					// stdout, which is unusable in the REPL for anything but
					// tiny snapshots.
					path := args[2]
					if err := os.WriteFile(path, data, 0644); err != nil {
						return fmt.Errorf("could not write snapshot to %q: %w", path, err)
					}
					if r.out.JSON() {
						r.out.Data(map[string]any{"id": id, "path": path, "bytes": len(data)})
						return nil
					}
					fmt.Println()
					fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Snapshot exported"))
					printKV("id", id)
					printKV("path", path)
					printKV("bytes", fmt.Sprintf("%d", len(data)))
					fmt.Println()
					return nil
				}
				if r.out.JSON() {
					r.out.Data(map[string]any{"id": id, "bytes": len(data)})
					return nil
				}
				fmt.Println(string(data))

			case "rm":
				if len(args) < 2 {
					return fmt.Errorf("usage: /snapshot rm <id>")
				}
				id := args[1]
				if err := r.gw.DeleteSnapshot(ctx, id); err != nil {
					return fmt.Errorf("could not delete snapshot: %w", err)
				}
				fmt.Println()
				fmt.Println(gcolor.HEX("#7fb88c").Sprint("  Snapshot deleted"))
				fmt.Println()

			default: // ls
				if len(args) > 0 && sub != "ls" {
					return fmt.Errorf("unknown subcommand %q — see /help", args[0])
				}
				snaps, err := r.gw.ListSnapshots(ctx, *r.session)
				if err != nil {
					return fmt.Errorf("could not fetch snapshots: %w", err)
				}
				if r.out.JSON() {
					r.out.Data(snaps)
					return nil
				}
				fmt.Println()
				gcolor.Bold.Print(gcolor.HEX("#e8b04a").Sprintf("  Snapshots (%d)\n\n", len(snaps)))
				if len(snaps) == 0 {
					fmt.Println(gcolor.HEX("#94a3b8").Sprint("  No snapshots found. Use /snapshot save to create one."))
					fmt.Println()
					return nil
				}
				for _, s := range snaps {
					fmt.Printf("  %s  %s\n",
						gcolor.HEX("#f4a261").Sprintf("%-36s", s.ID),
						gcolor.HEX("#94a3b8").Sprint(s.CreatedAt),
					)
				}
				fmt.Println()
			}
			return nil
		},
	}
}
