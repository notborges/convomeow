package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/notborges/convomeow/internal/core"
)

func (c *Client) group(ctx context.Context, args []string) error {
	if len(args) < 2 {
		return usage()
	}
	command, rest := args[0], args[1:]
	path := "/api/v1/conversations/" + url.PathEscape(rest[0]) + "/group"
	switch command {
	case "create":
		if len(rest) < 2 {
			return usage()
		}
		account, err := c.resolve(ctx, rest[0])
		if err != nil {
			return err
		}
		var group core.Group
		body := map[string]any{"name": rest[1], "participants": append([]string{}, rest[2:]...)}
		if err := c.do(ctx, http.MethodPost, "/api/v1/accounts/"+url.PathEscape(account.ID)+"/groups", body, &group); err != nil {
			return err
		}
		fmt.Printf("Created %s (%s)\n", group.Name, group.ConversationID)
		for _, failed := range group.FailedParticipants {
			fmt.Printf("Not added: %s  %s\n", participantLabel(failed.DisplayName, failed.ProviderID), failed.Status)
		}
		return nil
	case "info":
		if len(rest) != 1 {
			return usage()
		}
		var group core.Group
		if err := c.do(ctx, http.MethodGet, path, nil, &group); err != nil {
			return err
		}
		printGroup(group)
		return nil
	case "rename", "describe":
		if len(rest) < 2 && command == "rename" {
			return usage()
		}
		field := map[string]string{"rename": "name", "describe": "description"}[command]
		var group core.Group
		if err := c.do(ctx, http.MethodPatch, path, map[string]string{field: strings.Join(rest[1:], " ")}, &group); err != nil {
			return err
		}
		printGroup(group)
		return nil
	case "admins-only":
		if len(rest) != 3 || (rest[1] != "send" && rest[1] != "edit") || (rest[2] != "on" && rest[2] != "off") {
			return usage()
		}
		var group core.Group
		if err := c.do(ctx, http.MethodPatch, path, map[string]bool{"admins_only_" + rest[1]: rest[2] == "on"}, &group); err != nil {
			return err
		}
		printGroup(group)
		return nil
	case "add", "remove", "promote", "demote":
		if len(rest) < 2 {
			return usage()
		}
		var result page[core.GroupParticipantResult]
		if err := c.do(ctx, http.MethodPost, path+"/participants", map[string]any{"action": command, "participants": rest[1:]}, &result); err != nil {
			return err
		}
		for _, item := range result.Items {
			fmt.Printf("%-24s %s\n", participantLabel(item.DisplayName, item.ProviderID), item.Status)
		}
		return nil
	case "invite-link":
		method := http.MethodGet
		if len(rest) == 2 && rest[1] == "--reset" {
			method = http.MethodPost
		} else if len(rest) != 1 {
			return usage()
		}
		var link struct {
			URL string `json:"url"`
		}
		if err := c.do(ctx, method, path+"/invite-link", nil, &link); err != nil {
			return err
		}
		fmt.Println(link.URL)
		return nil
	case "leave":
		if len(rest) != 1 {
			return usage()
		}
		if err := c.do(ctx, http.MethodPost, path+"/leave", nil, nil); err != nil {
			return err
		}
		fmt.Println("Left the group.")
		return nil
	default:
		return usage()
	}
}

func printGroup(group core.Group) {
	fmt.Printf("%s (%s)\n", group.Name, group.ConversationID)
	if group.Description != "" {
		fmt.Println(group.Description)
	}
	fmt.Printf("Your role: %s · Admins only send: %t · Admins only edit: %t\n", group.OwnRole, group.AdminsOnlySend, group.AdminsOnlyEdit)
	for _, p := range group.Participants {
		name := participantLabel(p.DisplayName, p.ProviderID)
		if p.IsSelf {
			name += " (you)"
		}
		fmt.Printf("  %-30s %-7s %s\n", name, p.Role, p.ProviderID)
	}
}

func participantLabel(name, providerID string) string {
	if name != "" {
		return name
	}
	return providerID
}
