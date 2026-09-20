package cover

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestOnPathPackages(t *testing.T) {
	other := func(name string) string { return testOtherPkgPrefix + "/" + name }

	tests := []struct {
		name  string
		edges map[string][]string
		want  []string
	}{
		{
			name: "a dependency between two coverage targets is on the path",
			edges: map[string][]string{
				"coverA": {"bridge"},
				"bridge": {"coverB"},
				"coverB": nil,
			},
			want: []string{other("bridge")},
		},
		{
			name: "every dependency of a chain between coverage targets is on the path",
			edges: map[string][]string{
				"coverA": {"first"},
				"first":  {"second"},
				"second": {"coverB"},
				"coverB": nil,
			},
			want: []string{other("first"), other("second")},
		},
		{
			name: "a dependency that reaches no coverage target is off the path",
			edges: map[string][]string{
				"coverA": {"leaf", "bridge"},
				"leaf":   {"deeper"},
				"deeper": nil,
				"bridge": {"coverB"},
				"coverB": nil,
			},
			want: []string{other("bridge")},
		},
		{
			name: "a dependency no coverage target calls is off the path even if it reaches one",
			edges: map[string][]string{
				"entry":  {"coverA"},
				"coverA": nil,
			},
			want: nil,
		},
		{
			name: "a path through a cut-off package does not count",
			edges: map[string][]string{
				"coverA":      {"runtimeHook"},
				"runtimeHook": {"behind"},
				"behind":      {"coverB"},
				"coverB":      nil,
			},
			want: nil,
		},
		{
			name: "a direct call between coverage targets puts no dependency on the path",
			edges: map[string][]string{
				"coverA": {"coverB"},
				"coverB": nil,
			},
			want: nil,
		},
		{
			name: "a cycle of dependencies that leads to a coverage target is on the path",
			edges: map[string][]string{
				"coverA": {"ping"},
				"ping":   {"pong"},
				"pong":   {"ping", "coverB"},
				"coverB": nil,
			},
			want: []string{other("ping"), other("pong")},
		},
		{
			name: "a coverage target is not expanded on the way to a dependency",
			edges: map[string][]string{
				"coverA": {"coverB"},
				"coverB": {"bridge"},
				"bridge": {"coverC"},
				"coverC": nil,
			},
			want: []string{other("bridge")},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tg := newTestGraph(test.edges)
			followable := newFollowableCallees(tg.graph, emptyVTAGraph())
			fr := newFrontiers(tg.graph, followable, tg.coverPkgSet)
			got := sortedKeys(onPathPackages(tg.graph, followable, fr))
			if len(got) == 0 {
				got = nil
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("on-path packages (-want +got):\n%s", diff)
			}
		})
	}
}
