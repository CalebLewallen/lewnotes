package links

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	body := []byte("Intro [[Kitchen]] and ![[diagram.png|wide]]\nnext [[folder/Note#Heading|Alias]]\n" +
		"escaped \\[[no]] pipe [[a\\|b]]\nbroken [[open\ninline [[Two]]\n")
	got := Parse(body)
	want := []Ref{
		{Target: "Kitchen", Kind: KindLink, Line: 1},
		{Target: "diagram.png", Alias: "wide", Kind: KindEmbed, Line: 1},
		{Target: "folder/Note", Alias: "Alias", Fragment: "Heading", Kind: KindLink, Line: 2},
		{Target: "a|b", Kind: KindLink, Line: 3},
		{Target: "Two", Kind: KindLink, Line: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParseSkipsEmpty(t *testing.T) {
	for _, body := range []string{"[[]]", "[[   ]]", "[[|alias]]", "[[#heading]]"} {
		if refs := Parse([]byte(body)); len(refs) != 0 {
			t.Errorf("Parse(%q) = %+v, want none", body, refs)
		}
	}
}

func TestResolve(t *testing.T) {
	candidates := []Candidate{
		{ID: "1", Path: "notes/Kitchen.md"},
		{ID: "2", Path: "archive/Kitchen.md"},
		{ID: "3", Path: "notes/Unique.md"},
		{ID: "4", Path: "deep/folder/Garden.md"},
		{ID: "5", Path: "Garden.md"},
	}

	tests := []struct {
		target     string
		status     string
		id         string
		candidates []string
	}{
		{"notes/kitchen", "exact", "1", nil},
		{"notes/Kitchen.md", "exact", "1", nil},
		{"Unique", "suffix", "3", nil},
		{"folder/Garden", "suffix", "4", nil},
		{"Garden", "exact", "5", nil},
		{"Kitchen", "ambiguous", "", []string{"archive/Kitchen.md", "notes/Kitchen.md"}},
		{"Missing", "unresolved", "", nil},
		{"../etc/passwd", "unresolved", "", nil},
	}
	for _, tt := range tests {
		got := Resolve(tt.target, candidates)
		if got.Status != tt.status || got.ID != tt.id || !reflect.DeepEqual(got.Candidates, tt.candidates) {
			t.Errorf("Resolve(%q) = %+v, want status=%s id=%s candidates=%v",
				tt.target, got, tt.status, tt.id, tt.candidates)
		}
	}
}
