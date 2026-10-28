// Package ghguard validates argv before the real GitHub CLI runs. This is a
// command guard, not an OS sandbox: the OpenCode shell allowlist is still needed.
package ghguard

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var ErrDenied = errors.New("gh command denied by dicoco policy; use supported read commands, PR comments or approvals")

// Commands is also used to build OpenCode's shell allowlist. Unknown commands
// (including aliases and extensions) never reach gh's command dispatcher.
var Commands = []string{
	"repo list", "repo view", "pr list", "pr view", "pr diff", "pr checks", "pr status", "pr review", "pr comment",
	"issue list", "issue view", "issue status", "search code", "search commits", "search issues", "search prs", "search repos",
	"run list", "run view", "workflow list", "workflow view", "release list", "release view", "api", "help",
}

// Only known non-executing options are passed through. In particular, browser,
// editor, pager, file input/output, hostname and authentication options are absent.
const valueFlags = "json jq q template t repo R limit L state author assignee label base head search sort order owner language filename extension match visibility topic stars size created updated merged closed reviewed-by involves mentions milestone checks commit branch event status workflow user job name color body"
const boolFlags = "help h comments c patch name-only include-forks archived fork all a compact log log-failed verbose exit-status required latest exclude-drafts exclude-pre-releases comment approve yaml"

type option struct{ name, value string }

func parse(args []string, values, bools string) ([]string, []option, error) {
	v, b := map[string]bool{}, map[string]bool{}
	for _, s := range strings.Fields(values) {
		v[s] = true
	}
	for _, s := range strings.Fields(bools) {
		b[s] = true
	}
	var positional []string
	var opts []option
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "" || strings.ContainsRune(a, 0) {
			return nil, nil, ErrDenied
		}
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		if a == "--" {
			return nil, nil, ErrDenied
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-"), "=")
		// Short value options may be attached: -XGET, -fbody=hello, -Rorg/repo.
		if !strings.HasPrefix(a, "--") && len(a) > 2 && v[a[1:2]] {
			name = a[1:2]
			value = strings.TrimPrefix(a[2:], "=")
			hasValue = true
		}
		if v[name] {
			if !hasValue {
				i++
				if i == len(args) {
					return nil, nil, ErrDenied
				}
				value = args[i]
			}
			if value == "" || strings.HasPrefix(value, "-") {
				return nil, nil, ErrDenied
			}
		} else if b[name] {
			if hasValue {
				return nil, nil, ErrDenied
			}
		} else {
			return nil, nil, ErrDenied
		}
		opts = append(opts, option{name, value})
	}
	return positional, opts, nil
}

// Validate returns the original argv only after validating all arguments. No
// shell command string is constructed or re-evaluated by this package.
func Validate(args []string) ([]string, error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "--version") {
		return args, nil
	}
	if len(args) == 0 {
		return nil, ErrDenied
	}
	if args[0] == "api" {
		return validateAPI(args)
	}
	if args[0] == "help" {
		if len(args) == 1 {
			return args, nil
		}
		for _, c := range Commands {
			if strings.Join(args[1:], " ") == c {
				return args, nil
			}
		}
		return nil, ErrDenied
	}
	if len(args) < 2 {
		return nil, ErrDenied
	}
	command := strings.Join(args[:2], " ")
	allowed := false
	for _, c := range Commands {
		if command == c {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, ErrDenied
	}
	positions, opts, err := parse(args[2:], valueFlags, boolFlags)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(command, "search ") {
		for _, p := range positions {
			if strings.Contains(p, "://") {
				u, err := url.Parse(p)
				if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || !strings.HasPrefix(u.Path, "/wafflestudio/") {
					return nil, ErrDenied
				}
			} else if command == "repo view" && !repo.MatchString(p) {
				return nil, ErrDenied
			}
		}
	}
	for _, o := range opts {
		if o.name == "repo" || o.name == "R" {
			if !repo.MatchString(o.value) {
				return nil, ErrDenied
			}
		}
	}
	// Write commands cannot start an editor, consume stdin, choose arbitrary
	// hosts, edit/delete previous comments or silently select a local PR.
	if command == "pr review" || command == "pr comment" {
		if len(positions) != 1 || !prURL.MatchString(positions[0]) {
			return nil, ErrDenied
		}
		mode, body := "", false
		for _, o := range opts {
			switch o.name {
			case "body":
				body = strings.TrimSpace(o.value) != ""
			case "comment", "approve", "a", "c":
				if mode != "" {
					return nil, ErrDenied
				}
				mode = o.name
			case "help", "h":
			default:
				return nil, ErrDenied
			}
		}
		if command == "pr review" && (mode == "" || ((mode == "comment" || mode == "c") && !body)) {
			return nil, ErrDenied
		}
		if command == "pr comment" && (!body || mode != "") {
			return nil, ErrDenied
		}
	}
	return args, nil
}

var repo = regexp.MustCompile(`^wafflestudio/[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)
var prURL = regexp.MustCompile(`^https://github\.com/wafflestudio/[A-Za-z0-9_-][A-Za-z0-9_.-]*/pull/[1-9][0-9]*$`)
var comments = regexp.MustCompile(`^repos/wafflestudio/[A-Za-z0-9_-][A-Za-z0-9_.-]*/pulls/[1-9][0-9]*/comments$`)

func validateAPI(args []string) ([]string, error) {
	positions, opts, err := parse(args[1:], "method X field F raw-field f header H jq q template t", "paginate slurp include i silent help")
	if err != nil || len(positions) != 1 {
		return nil, ErrDenied
	}
	endpoint := strings.TrimPrefix(positions[0], "/")
	// Reject URLs, encoded/path-normalized routes, GraphQL and placeholders.
	u, err := url.Parse(endpoint)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || strings.ContainsAny(endpoint, "%\\{}") {
		return nil, ErrDenied
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "" || part == "." || part == ".." {
			return nil, ErrDenied
		}
	}
	if !(strings.HasPrefix(u.Path, "repos/wafflestudio/") || strings.HasPrefix(u.Path, "orgs/wafflestudio/") || strings.HasPrefix(u.Path, "search/")) {
		return nil, ErrDenied
	}
	method := "GET"
	methodSeen, fields := false, false
	for _, o := range opts {
		switch o.name {
		case "method", "X":
			if methodSeen {
				return nil, ErrDenied
			}
			methodSeen = true
			method = strings.ToUpper(o.value)
		case "field", "F", "raw-field", "f":
			fields = true
			key, value, ok := strings.Cut(o.value, "=")
			if !ok || key == "" || strings.HasPrefix(value, "@") || strings.ContainsAny(o.value, "{}") {
				return nil, ErrDenied
			}
		case "header", "H":
			key, value, ok := strings.Cut(o.value, ":")
			if !ok || strings.ContainsAny(o.value, "\r\n") {
				return nil, ErrDenied
			}
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.TrimSpace(value)
			if key != "accept" || (value != "application/vnd.github+json" && value != "application/vnd.github.raw+json" && value != "application/vnd.github.diff" && value != "application/vnd.github.patch") {
				return nil, ErrDenied
			}
		}
	}
	if fields && !methodSeen {
		return nil, ErrDenied
	} // gh would implicitly POST.
	if method != "GET" {
		if method != "POST" || !comments.MatchString(endpoint) {
			return nil, ErrDenied
		}
		seen := map[string]string{}
		for _, o := range opts {
			switch o.name {
			case "method", "X", "jq", "q":
			case "raw-field", "f", "field", "F":
				k, v, _ := strings.Cut(o.value, "=")
				if _, exists := seen[k]; exists {
					return nil, ErrDenied
				}
				switch k {
				case "body", "commit_id", "path", "line", "side", "start_line", "start_side", "in_reply_to":
				default:
					return nil, ErrDenied
				}
				seen[k] = v
			default:
				return nil, ErrDenied
			}
		}
		if strings.TrimSpace(seen["body"]) == "" {
			return nil, ErrDenied
		}
	}
	return args, nil
}
