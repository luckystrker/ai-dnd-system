package telegram

import "testing"

func TestIsCommandRequiresCommandName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		text string
		want bool
	}{
		{"/join", true},
		{"/join@dnd_bot сделай", true},
		{"/join@DND_BOT", true},
		{"/join@other_bot", false},
		{"/", false},
		{"/@dnd_bot", false},
		{"/join!", false},
		{"///", false},
		{" /start", true},
		{"текст /join", false},
		{"join", false},
	}
	for _, tc := range cases {
		message := &Message{Text: tc.text}
		if got := message.IsCommand("dnd_bot"); got != tc.want {
			t.Fatalf("IsCommand(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
	if name := (&Message{Text: "/"}).CommandName(); name != "" {
		t.Fatalf("CommandName(/) = %q", name)
	}
	if name := (&Message{Text: "/join@dnd_bot"}).CommandName(); name != "join" {
		t.Fatalf("CommandName = %q", name)
	}
}
