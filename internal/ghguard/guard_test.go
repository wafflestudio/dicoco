package ghguard

import (
	"reflect"
	"testing"
)

func TestAllowedOperations(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"help", "pr", "review"},
		{"repo", "list", "wafflestudio", "--limit", "50", "--json", "name,pushedAt,url"},
		{"pr", "view", "https://github.com/wafflestudio/dicoco/pull/1", "--json", "headRefOid"},
		{"pr", "diff", "https://github.com/wafflestudio/dicoco/pull/1"},
		{"search", "code", "login", "--repo", "wafflestudio/dicoco"},
		{"api", "/orgs/wafflestudio/repos", "--paginate", "--jq", ".[].name"},
		{"api", "--method", "GET", "repos/wafflestudio/dicoco/contents/README.md?ref=main", "-H", "Accept: application/vnd.github.raw+json"},
		{"api", "-X=GET", "search/code", "-fq=login org:wafflestudio"},
		{"api", "-XGET", "search/code", "--raw-field=q=login org:wafflestudio"},
		{"api", "--method=POST", "repos/wafflestudio/dicoco/pulls/1/comments", "-fbody=lgtm!", "-fcommit_id=abc", "-fpath=main.go", "-Fline=10", "-fside=RIGHT"},
		{"pr", "review", "https://github.com/wafflestudio/dicoco/pull/1", "--comment", "--body", "Please check --request-changes in this code; `shell` is plain text."},
		{"pr", "review", "https://github.com/wafflestudio/dicoco/pull/1", "--approve"},
		{"pr", "review", "https://github.com/wafflestudio/dicoco/pull/1", "-a"},
		{"pr", "comment", "https://github.com/wafflestudio/dicoco/pull/1", "--body=lgtm!"},
	} {
		t.Run(args[0]+"/"+args[len(args)-1], func(t *testing.T) {
			got, err := Validate(args)
			if err != nil || !reflect.DeepEqual(got, args) {
				t.Fatalf("valid command rejected: %v: %v", args, err)
			}
		})
	}
}

func TestBypassesDenied(t *testing.T) {
	pr := "https://github.com/wafflestudio/dicoco/pull/1"
	endpoint := "repos/wafflestudio/dicoco/pulls/1/comments"
	cases := [][]string{
		nil, {"alias", "set", "--shell", "x", "printf unsafe"}, {"x"},
		{"extension", "exec", "x"}, {"auth", "token"}, {"config", "set", "pager", "sh"},
		{"--repo", "wafflestudio/dicoco", "pr", "close", "1"},
		{"pr", "close", pr}, {"pr", "merge", pr}, {"repo", "delete", "wafflestudio/dicoco"},
		{"pr", "review", pr, "--request-changes", "--body", "x"},
		{"pr", "review", pr, "-ar", "--body", "x"},
		{"pr", "review", pr, "--approve", "-F", "/var/run/secrets/key"},
		{"pr", "review", pr, "--approve", "--body-file=/etc/passwd"},
		{"pr", "review", pr}, {"pr", "review", "https://evil.example/x/pull/1", "--approve"},
		{"pr", "comment", pr, "--body", "x", "--delete-last"},
		{"pr", "view", pr, "--web"}, {"pr", "diff", pr, "--output", "/tmp/file"},
		{"pr", "view", pr, "-w", "value"},
		{"pr", "view", "https://evil.example/wafflestudio/dicoco/pull/1"},
		{"repo", "view", "evil.example/wafflestudio/dicoco"},
		{"search", "repos", "dicoco", "--archived", "--web"},
		{"api", endpoint, "-fbody=implicit POST"}, {"api", endpoint, "--raw-field=body=x"},
		{"api", endpoint, "-X=PATCH", "-fstate=closed"},
		{"api", "graphql", "-fquery=mutation { x }"},
		{"api", "graphql", "-fquery={viewer{login}}"},
		{"api", "--method", "GET", endpoint, "-X=PATCH"},
		{"api", "--method", "POST", endpoint, "-fbody=x", "--hostname", "evil.example"},
		{"api", "--method", "POST", endpoint, "-fbody=x", "--paginate"},
		{"api", "--method", "POST", endpoint + "?x=1", "-fbody=x"},
		{"api", "--method", "POST", endpoint + "/../../reviews", "-fbody=x"},
		{"api", "--method", "POST", "repos/wafflestudio/x/pulls/1/../2/comments", "-fbody=x"},
		{"api", "--method", "POST", "repos/wafflestudio/x/pulls/1/%2e%2e/comments", "-fbody=x"},
		{"api", "--method", "POST", endpoint, "-fbody=x", "-fevent=APPROVE"},
		{"api", "--method", "GET", endpoint, "--input", "-"},
		{"api", "--method", "GET", endpoint, "-Fbody=@/etc/passwd"},
		{"api", "--method", "GET", endpoint, "-fbody=@/etc/passwd"},
		{"api", "--method", "GET", endpoint, "-H", "X-HTTP-Method-Override: DELETE"},
		{"api", "--method", "GET", endpoint, "-H", "Accept: application/vnd.github+json\r\nX-HTTP-Method-Override: DELETE"},
		{"api", "https://evil.example/"}, {"api", "//evil.example/"},
		{"api", endpoint, "--cache", "1h"}, {"help", "alias", "x"},
	}
	for _, args := range cases {
		if _, err := Validate(args); err == nil {
			t.Errorf("accepted bypass: %q", args)
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "patch", "PaTcH"} {
		for _, flags := range [][]string{{"-X", method}, {"-X" + method}, {"-X=" + method}, {"--method", method}, {"--method=" + method}} {
			args := append([]string{"api", "repos/wafflestudio/dicoco/pulls/1"}, flags...)
			if _, err := Validate(args); err == nil {
				t.Errorf("accepted API write: %q", args)
			}
		}
	}
}

func FuzzValidate(f *testing.F) {
	f.Add("-X=PATCH", "-fstate=closed")
	f.Add("--method=GET", "--paginate")
	f.Fuzz(func(t *testing.T, a, b string) {
		Validate([]string{"api", "repos/wafflestudio/dicoco/pulls/1", a, b})
	})
}
