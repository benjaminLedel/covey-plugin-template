// Command example is a Covey target-system plugin: ordinary Go, compiled to
// WebAssembly.
//
//	make build     # -> example.wasm, and the digest to put in the catalogue
//	make lint      # the same check Covey runs at install time
//
// Replace the plugin below with yours. The parts worth keeping are the shape:
// Describe says what you are, Execute does the work, and every call to the
// target system goes through covey.Fetch — because you have no network of your
// own, and no credential.
package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"example.com/covey-plugin/covey"
)

func main() { covey.Run(plugin{}) }

type plugin struct{}

func (plugin) Describe() covey.Description {
	return covey.Description{
		Name:        "example",
		Label:       "Example tracker",
		Description: "Template plugin: find issues, read them, comment.",
		Category:    "ticketing",
		// The vocabulary ACCESS.md may use. An agent gets only the actions
		// whose scope it was granted.
		Scopes: []string{"read", "comment"},
		// Where the brokered token goes. This is the DEFAULT; delete the block
		// unless your system wants something else (e.g. a private header).
		Auth: covey.AuthDesc{Header: "Authorization", Format: "Bearer {token}"},
		// Declare only what you implement.
		Probe: true,
		Poll:  true,
		Actions: []covey.ActionDesc{
			{Name: "list_issues", Scope: "read", Doc: "find open issues; params: query (optional)"},
			{Name: "get_issue", Scope: "read", Doc: "read one issue with its comments; params: id"},
			{
				Name: "comment", Scope: "comment", Doc: "write a comment; params: id, body",
				// A separate guard-rail subject, because writing to a customer
				// is the thing a person may want to gate on its own.
				Subject: "comment_external",
			},
		},
	}
}

func (plugin) Execute(action string, params json.RawMessage) (any, error) {
	var p struct {
		ID    int    `json:"id"`
		Body  string `json:"body"`
		Query string `json:"query"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("params: %w", err)
		}
	}

	switch action {
	case "list_issues":
		req := covey.Request{Method: "GET", Path: "/issues"}
		if p.Query != "" {
			req.Query = map[string]string{"q": p.Query}
		}
		resp := covey.Fetch(req)
		if !resp.OK() {
			return nil, statusError("list_issues", resp)
		}
		var issues []map[string]any
		if err := resp.JSON(&issues); err != nil {
			return nil, err
		}
		return issues, nil

	case "get_issue":
		if p.ID == 0 {
			return nil, fmt.Errorf("get_issue needs an id")
		}
		resp := covey.Get(fmt.Sprintf("/issues/%d", p.ID))
		if !resp.OK() {
			return nil, statusError("get_issue", resp)
		}
		var issue map[string]any
		if err := resp.JSON(&issue); err != nil {
			return nil, err
		}
		return issue, nil

	case "comment":
		if p.ID == 0 || strings.TrimSpace(p.Body) == "" {
			return nil, fmt.Errorf("comment needs an id and a body")
		}
		resp := covey.Post(fmt.Sprintf("/issues/%d/comments", p.ID),
			map[string]string{"body": p.Body})
		if !resp.OK() {
			return nil, statusError("comment", resp)
		}
		return "commented", nil

	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
}

// Probe: one read-only call that says whose credential this is. What it returns
// is shown to a person as-is, so keep it short and recognisable.
func (plugin) Probe() (string, error) {
	resp := covey.Get("/user")
	if !resp.OK() {
		return "", statusError("probe", resp)
	}
	var me struct {
		Login string `json:"login"`
	}
	if err := resp.JSON(&me); err != nil {
		return "", err
	}
	if me.Login == "" {
		// The call worked — a missing field is the plugin's problem, not the
		// credential's, and saying otherwise sends people looking in the wrong
		// place.
		return "ok", nil
	}
	return me.Login, nil
}

// Poll answers `nur-wenn: example` in HEARTBEAT.md. The signature is what keeps
// an agent from being woken every few minutes for the same news: it should
// change when the work does, and not otherwise.
func (plugin) Poll(kind string) (bool, string, error) {
	resp := covey.Get("/issues?state=open&assigned=me")
	if !resp.OK() {
		return false, "", statusError("poll", resp)
	}
	var issues []struct {
		ID        int    `json:"id"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := resp.JSON(&issues); err != nil {
		return false, "", err
	}
	parts := make([]string, 0, len(issues))
	for _, i := range issues {
		parts = append(parts, fmt.Sprintf("%d@%s", i.ID, i.UpdatedAt))
	}
	return len(issues) > 0, strings.Join(parts, ","), nil
}

// statusError turns a failed call into a message an agent can act on. Include
// what the system said — "HTTP 422" alone tells nobody which field was wrong.
func statusError(what string, resp covey.Response) error {
	if resp.Error != "" {
		return fmt.Errorf("%s: %s", what, resp.Error)
	}
	detail := resp.Text
	if detail == "" && len(resp.Body) > 0 {
		detail = string(resp.Body)
	}
	if len(detail) > 300 {
		detail = detail[:300] + "…"
	}
	return fmt.Errorf("%s: HTTP %d: %s", what, resp.Status, detail)
}
