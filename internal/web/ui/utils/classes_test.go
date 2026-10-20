package utils

import "testing"

func TestCN(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []any
		want string
	}{
		{"override", []any{"p-2 text-sm", "p-4"}, "text-sm p-4"},
		{"v4 variable", []any{"px-2", "px-(--card-spacing)"}, "px-(--card-spacing)"},
		{"v4 important", []any{"hover:p-2!", "hover:p-4!"}, "hover:p-4!"},
		{"nested conditionals", []any{[]any{"cn-input", []string{"w-full"}}, map[string]bool{"hidden": false, "font-mono": true}}, "cn-input w-full font-mono"},
		{"preserve partial padding order", []any{"p-2 pb-11"}, "p-2 pb-11"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CN(tc.in...); got != tc.want {
				t.Fatalf("CN() = %q, want %q", got, tc.want)
			}
			if got := CN(CN(tc.in...)); got != tc.want {
				t.Fatalf("repeated merge = %q, want %q", got, tc.want)
			}
		})
	}
}
