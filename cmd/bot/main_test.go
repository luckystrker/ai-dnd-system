package main

import "testing"

func TestResolveMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mode  string
		token string
		want  string
		err   bool
	}{
		{mode: "auto", token: "123:abc", want: modeTelegram},
		{mode: "auto", token: "", want: modeRepl},
		{mode: "", token: "", want: modeRepl},
		{mode: "telegram", token: "123:abc", want: modeTelegram},
		{mode: "telegram", token: "", err: true},
		{mode: "repl", token: "123:abc", want: modeRepl},
		{mode: "bogus", token: "", err: true},
	}
	for _, tc := range cases {
		got, err := resolveMode(tc.mode, tc.token)
		if tc.err {
			if err == nil {
				t.Fatalf("%q/%q: ожидалась ошибка", tc.mode, tc.token)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%q/%q = %q, %v; want %q", tc.mode, tc.token, got, err, tc.want)
		}
	}
}
