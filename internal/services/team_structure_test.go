package services

import "testing"

func TestValidateTeamParent(t *testing.T) {
	cases := []struct {
		name, team, parent string
		chain              []string // parent, then its ancestors
		cycle              bool
	}{
		{"top level", "a", "", nil, false},
		{"under itself", "a", "a", []string{"a"}, true},
		{"under a sibling", "a", "b", []string{"b", "root"}, false},
		{"under its child", "a", "c", []string{"c", "a", "root"}, true},
		{"under a grandchild", "a", "g", []string{"g", "c", "a"}, true},
		{"under an unrelated deep team", "a", "x", []string{"x", "y", "z"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateTeamParent(c.team, c.parent, c.chain)
			if (err != nil) != c.cycle {
				t.Errorf("ValidateTeamParent(%q, %q, %v) = %v, want cycle=%v", c.team, c.parent, c.chain, err, c.cycle)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"LLM Router":             "llm-router",
		"  Agents & workflows  ": "agents-workflows",
		"Document processing!!":  "document-processing",
		"---":                    "",
		"Ünïcödé Team":           "n-c-d-team",
		"a__b":                   "a-b",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	long := Slugify("x" + string(make([]byte, 0)) + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if len(long) > 60 {
		t.Errorf("slug longer than 60: %d", len(long))
	}
	for s, ok := range map[string]bool{"eng": true, "llm-router": true, "": false, "Eng": false, "a--b": false, "-a": false, "a b": false} {
		if ValidSlug(s) != ok {
			t.Errorf("ValidSlug(%q) = %v, want %v", s, !ok, ok)
		}
	}
}
