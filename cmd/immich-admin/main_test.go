package main

import "testing"

func TestShowsRootHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: nil, want: true},
		{name: "help", args: []string{"help"}, want: true},
		{name: "long help", args: []string{"--help"}, want: true},
		{name: "short help", args: []string{"-h"}, want: true},
		{name: "subcommand", args: []string{"albums"}, want: false},
		{name: "nested help", args: []string{"albums", "--help"}, want: false},
		{name: "help target", args: []string{"help", "albums"}, want: false},
		{name: "skill itself", args: []string{"return-agent-skill"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := showsRootHelp(tc.args); got != tc.want {
				t.Errorf("showsRootHelp(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
