package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestNormalizeRepo(t *testing.T) {
	ok := map[string]string{
		"Tributary-ai-services/aiqg-ui": "tributary-ai-services/aiqg-ui",
		"  owner/name  ":                "owner/name",
		"a/b.c_d-e":                     "a/b.c_d-e",
		"o/.github":                     "o/.github",
	}
	for in, want := range ok {
		got, err := NormalizeRepo(in)
		if err != nil || got != want {
			t.Errorf("NormalizeRepo(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "owner", "owner/", "/name", "a/b/c", "-owner/x", "owner-/x", "ow--ner/x",
		"own er/x", "o/na me", "o/.", "o/..", "https://github.com/o/n", strings.Repeat("a", 40) + "/x", "o/" + strings.Repeat("n", 101)} {
		if got, err := NormalizeRepo(in); err == nil {
			t.Errorf("NormalizeRepo(%q) = %q, want an error", in, got)
		}
	}
}

func TestNormalizeSourceApp(t *testing.T) {
	for in, want := range map[string]string{"claude-code": "claude-code", " AiqgUI ": "AiqgUI", "svc:v2@prod+x": "svc:v2@prod+x"} {
		if got, err := NormalizeSourceApp(in); err != nil || got != want {
			t.Errorf("NormalizeSourceApp(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", " ", "-x", "a b", "a/b", "a\nb", strings.Repeat("x", MaxSourceAppLen+1)} {
		if got, err := NormalizeSourceApp(in); err == nil {
			t.Errorf("NormalizeSourceApp(%q) = %q, want an error", in, got)
		}
	}
}

func TestNormalizeOwnedList(t *testing.T) {
	got, err := NormalizeOwnedList(OwnedKindRepo, []string{"B/x", "a/y", "b/X", "a/y"})
	if err != nil || fmt.Sprint(got) != "[a/y b/x]" {
		t.Errorf("repos = %v, %v; want [a/y b/x]", got, err)
	}
	// Source apps are case-sensitive: both stay.
	got, err = NormalizeOwnedList(OwnedKindSourceApp, []string{"App", "app", "app"})
	if err != nil || fmt.Sprint(got) != "[App app]" {
		t.Errorf("source apps = %v, %v; want [App app]", got, err)
	}
	if got, err := NormalizeOwnedList(OwnedKindRepo, nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("nil list = %#v, %v; want empty non-nil", got, err)
	}
	if _, err := NormalizeOwnedList(OwnedKindRepo, []string{"ok/x", "bad"}); err == nil {
		t.Error("one bad entry should fail the list")
	}
	// The bound counts distinct entries.
	many := make([]string, 0, MaxTeamSourceApps+1)
	for i := 0; i <= MaxTeamSourceApps; i++ {
		many = append(many, fmt.Sprintf("app-%d", i))
	}
	if _, err := NormalizeOwnedList(OwnedKindSourceApp, many); err == nil {
		t.Errorf("%d source apps should exceed the bound", len(many))
	}
	if _, err := NormalizeOwnedList(OwnedKindSourceApp, append(many[:MaxTeamSourceApps:MaxTeamSourceApps], "app-0")); err != nil {
		t.Errorf("duplicates must not count against the bound: %v", err)
	}
}

func TestOwnedDiff(t *testing.T) {
	add, rm := OwnedDiff([]string{"a", "b", "c"}, []string{"b", "d", "a0"})
	if fmt.Sprint(add) != "[a0 d]" || fmt.Sprint(rm) != "[a c]" {
		t.Errorf("diff = +%v -%v", add, rm)
	}
	if add, rm := OwnedDiff([]string{"x"}, []string{"x"}); add != nil || rm != nil {
		t.Errorf("no-op diff = +%v -%v", add, rm)
	}
}

// The new fields are additive: an event's team always carries repos and
// source_apps (never omitted), and owned is omitted on events that are not
// about a repo or source app.
func TestOrgChangeEventOwnedJSON(t *testing.T) {
	ev := OrgChangeEvent{EventID: "e", Type: EvTeamRenamed, OrgID: "o",
		Team: &OrgChangeTeam{ID: "t", Name: "n", Slug: "s", Repos: []string{}, SourceApps: []string{}}}
	b, _ := json.Marshal(ev)
	if !strings.Contains(string(b), `"repos":[]`) || !strings.Contains(string(b), `"source_apps":[]`) || strings.Contains(string(b), `"owned"`) {
		t.Errorf("renamed event JSON = %s", b)
	}
	ev.Type, ev.Owned = EvTeamRepoAdded, &OrgChangeOwned{Kind: OwnedKindRepo, Key: "o/r"}
	b, _ = json.Marshal(ev)
	if !strings.Contains(string(b), `"owned":{"kind":"repo","key":"o/r"}`) {
		t.Errorf("repo event JSON = %s", b)
	}
}
