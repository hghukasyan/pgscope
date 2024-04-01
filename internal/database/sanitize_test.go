package database

import (
	"strings"
	"testing"
)

func TestSanitizeQueryPreview(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "collapse whitespace", in: "SELECT  *\nFROM  t", max: 120, want: "SELECT * FROM t"},
		{name: "truncate", in: "abcdefghijklmnopqrstuvwxyz", max: 10, want: "abcdefg..."},
		{name: "empty", in: "   ", max: 10, want: ""},
		{name: "no secrets expansion", in: "SELECT $1, $2", max: 120, want: "SELECT $1, $2"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeQueryPreview(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestRedactDSN(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "url",
			in:   "postgres://alice:s3cret@localhost:5432/app?sslmode=disable",
			want: "postgres://alice:****@localhost:5432/app?sslmode=disable",
		},
		{
			name: "keyword",
			in:   "host=localhost user=alice password=s3cret dbname=app",
			want: "host=localhost user=alice password=**** dbname=app",
		},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := RedactDSN(tc.in)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if strings.Contains(got, "s3cret") {
				t.Fatalf("redacted dsn still contains secret: %q", got)
			}
		})
	}
}
