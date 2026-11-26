// Package covey is the small amount of glue between the Covey plugin protocol
// and the code you actually want to write.
//
// The protocol itself is stdio and line-based: Covey writes one invocation to
// stdin, your plugin writes messages to stdout, and it ends with a result or an
// error. That is deliberately simple enough to implement by hand — this package
// exists so you do not have to.
//
// What you never touch, and cannot: the network, the filesystem, and the
// credential. Your plugin says "GET /issues/7"; Covey adds the base URL and the
// token the organisation stored. Which is why a plugin cannot leak a token — it
// never has one, and a wasm module has no socket to leak it through.
package covey

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// Plugin is what you implement. Only Describe and Execute are required; the
// rest are optional and are declared through Description.
type Plugin interface {
	// Describe says what this plugin is. Covey asks once, when the module is
	// installed, and stores the answer — so the store can list it without
	// running it.
	Describe() Description
	// Execute performs one action for an agent.
	Execute(action string, params json.RawMessage) (any, error)
}

// Prober is optional: one cheap, read-only call that shows whether the stored
// credentials work, and as whom. Declare it with Description.Probe.
//
// Read-only by contract. A connection test that leaves traces in somebody's
// live system is a poor kind of test.
type Prober interface {
	Probe() (string, error)
}

// Poller is optional: the cheap up-front check for `nur-wenn:` in HEARTBEAT.md.
// Declare it with Description.Poll.
//
// The signature should describe WHAT you found — an id, a timestamp, the id of
// the newest comment. Covey remembers it and only wakes the agent again when it
// changes, so the same piece of news does not wake anybody twice.
type Poller interface {
	Poll(kind string) (hasWork bool, signature string, err error)
}

// Documenter is optional: the documentation an agent with these scopes should
// see. Without it, Covey renders the action list from Description.
//
// It is worth implementing. The doc sits in the context of EVERY turn, so what
// an agent cannot use is not paid for once but on every one.
type Documenter interface {
	PromptDoc(scopes []string) string
}

// Description is what Describe returns.
type Description struct {
	// Name is the plugin's identity: the credential prefix (<name>_token,
	// <name>_url), the word in ACCESS.md, the guard-rail subject prefix.
	// Lower case, [a-z][a-z0-9_-]{1,31}.
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	// Category places it in the store: ticketing, code, communication, files,
	// web, dev, other.
	Category string       `json:"category,omitempty"`
	Actions  []ActionDesc `json:"actions,omitempty"`
	// Scopes is the vocabulary ACCESS.md may use for this plugin.
	Scopes []string `json:"scopes,omitempty"`
	// Auth says where the host should put the token. Empty =
	// "Authorization: Bearer {token}".
	Auth AuthDesc `json:"auth,omitempty"`
	// Credentials says whether the plugin needs a brokered secret at all:
	// CredentialsRequired (the default) — <name>_token and <name>_url;
	// CredentialsOptional — granted without a secret, whatever is stored is
	// passed on; CredentialsNone — never needs one, for a plugin that reaches
	// only hosts it declares. Both of the latter make <name>_url optional. An
	// unknown value reads as the default. Needs a covey that knows the field;
	// an older one ignores it and keeps asking for both secrets.
	Credentials string `json:"credentials,omitempty"`
	// Probe/Poll: set these only if you implement the matching interface.
	// Claiming a capability you do not have earns the operator a button that
	// can only fail.
	Probe bool `json:"probe,omitempty"`
	Poll  bool `json:"poll,omitempty"`
}

// The values of Description.Credentials.
const (
	CredentialsRequired = ""
	CredentialsOptional = "optional"
	CredentialsNone     = "none"
)

type AuthDesc struct {
	Header string `json:"header,omitempty"`
	Format string `json:"format,omitempty"`
}

// ActionDesc describes one action. Doc is the line an agent reads; Scope is the
// access level it belongs to; Subject names the guard-rail subject when the
// default (<name>:<action>) is too coarse — reply_external vs reply_internal is
// the classic case.
type ActionDesc struct {
	Name    string `json:"name"`
	Doc     string `json:"doc,omitempty"`
	Subject string `json:"subject,omitempty"`
	Scope   string `json:"scope,omitempty"`
}

// Request is an HTTP call you want made against the target system. You give a
// PATH — you cannot name a host, and you do not need to.
type Request struct {
	Method string            `json:"method"`
	Path   string            `json:"path"`
	Query  map[string]string `json:"query,omitempty"`
	Header map[string]string `json:"header,omitempty"`
	Body   json.RawMessage   `json:"body,omitempty"`
}

// Response is what came back. Error is set when the request could not be made
// at all (DNS, timeout, a refused egress) — status codes are not errors here,
// because only your plugin knows whether a 404 is a failure or an answer.
type Response struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"`
	Text   string          `json:"text,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// OK is true for 2xx and no transport error.
func (r Response) OK() bool { return r.Error == "" && r.Status >= 200 && r.Status < 300 }

// JSON unmarshals the response body into v.
func (r Response) JSON(v any) error {
	if r.Error != "" {
		return fmt.Errorf("%s", r.Error)
	}
	if len(r.Body) == 0 {
		return fmt.Errorf("empty response body (HTTP %d)", r.Status)
	}
	return json.Unmarshal(r.Body, v)
}

var stdin *bufio.Reader

// Fetch asks Covey to perform a request against the target system.
func Fetch(req Request) Response {
	emit(map[string]any{"fetch": req})
	line, err := stdin.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Response{Error: "covey closed the connection"}
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{Error: "covey's answer is not JSON: " + err.Error()}
	}
	return resp
}

// Get is Fetch for the common case.
func Get(path string) Response { return Fetch(Request{Method: "GET", Path: path}) }

// Post sends a JSON body.
func Post(path string, body any) Response {
	raw, err := json.Marshal(body)
	if err != nil {
		return Response{Error: err.Error()}
	}
	return Fetch(Request{Method: "POST", Path: path, Body: raw})
}

// Log writes a diagnostic line for the operator. It never reaches the agent's
// context — say what would help somebody debugging, not what the agent should
// know.
func Log(format string, args ...any) {
	emit(map[string]any{"log": fmt.Sprintf(format, args...)})
}

// Run is the plugin's main. Hand it your implementation and it speaks the
// protocol for you:
//
//	func main() { covey.Run(myPlugin{}) }
func Run(p Plugin) {
	stdin = bufio.NewReaderSize(os.Stdin, 1<<20)
	line, err := stdin.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		fail("no invocation on stdin")
		return
	}
	var inv struct {
		Op     string          `json:"op"`
		Action string          `json:"action"`
		Params json.RawMessage `json:"params"`
		Kind   string          `json:"kind"`
		Scopes []string        `json:"scopes"`
	}
	if err := json.Unmarshal(line, &inv); err != nil {
		fail("invocation is not JSON: " + err.Error())
		return
	}

	switch inv.Op {
	case "describe":
		emit(map[string]any{"describe": p.Describe()})

	case "execute":
		out, err := p.Execute(inv.Action, inv.Params)
		if err != nil {
			fail(err.Error())
			return
		}
		result(out)

	case "probe":
		pr, ok := p.(Prober)
		if !ok {
			fail("this plugin has no connection test")
			return
		}
		who, err := pr.Probe()
		if err != nil {
			fail(err.Error())
			return
		}
		result(who)

	case "poll":
		pl, ok := p.(Poller)
		if !ok {
			// Fail-open: a plugin that cannot answer must not silence a
			// heartbeat, or work would quietly pile up.
			result(map[string]any{"has_work": true})
			return
		}
		has, sig, err := pl.Poll(inv.Kind)
		if err != nil {
			fail(err.Error())
			return
		}
		result(map[string]any{"has_work": has, "signature": sig})

	case "prompt_doc":
		if d, ok := p.(Documenter); ok {
			result(d.PromptDoc(inv.Scopes))
			return
		}
		result(defaultDoc(p.Describe(), inv.Scopes))

	default:
		fail("unknown op " + inv.Op)
	}
}

// defaultDoc renders the action list, narrowed to the granted scopes.
func defaultDoc(d Description, scopes []string) string {
	granted := map[string]bool{}
	for _, s := range scopes {
		granted[s] = true
	}
	out := "Available " + d.Name + " actions:\n"
	var n int
	for _, a := range d.Actions {
		if a.Scope != "" && len(scopes) > 0 && !granted[a.Scope] {
			continue
		}
		n++
		out += "- " + a.Name
		if a.Doc != "" {
			out += ": " + a.Doc
		}
		out += "\n"
	}
	if n == 0 {
		return "No " + d.Name + " actions are available to you."
	}
	return out
}

func emit(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(map[string]string{"error": "cannot encode message: " + err.Error()})
	}
	os.Stdout.Write(append(b, '\n'))
}

func result(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fail("cannot encode result: " + err.Error())
		return
	}
	emit(map[string]any{"result": json.RawMessage(b)})
}

func fail(msg string) { emit(map[string]any{"error": msg}) }
