package observability

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMaskEmail(t *testing.T) {
	cases := []struct {
		name  string
		email string
		want  string
	}{
		{name: "simple", email: "alice@example.com", want: "a***@example.com"},
		{name: "single char local part", email: "a@example.com", want: "a***@example.com"},
		{name: "dotted local part", email: "alice.private@example.com", want: "a***@example.com"},
		{name: "no at sign", email: "no-at-sign", want: "***"},
		{name: "empty", email: "", want: "***"},
		{name: "missing local part", email: "@example.com", want: "***"},
		{name: "quoted local part", email: `"quoted"@example.com`, want: `"***@example.com`},
		{name: "unicode local part", email: "ünïcödé@例え.jp", want: "ü***@例え.jp"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskEmail(tc.email); got != tc.want {
				t.Errorf("MaskEmail(%q) = %q, want %q", tc.email, got, tc.want)
			}
		})
	}
}

// The masked form must never contain the full local part, whatever its length.
func TestMaskEmail_NeverLeaksLocalPart(t *testing.T) {
	secrets := []string{
		"alice@example.com",
		"alice.private@example.com",
		"averyveryverylongusername@example.com",
		"a.b.c.d.e@example.co.uk",
	}

	for _, email := range secrets {
		t.Run(email, func(t *testing.T) {
			masked := MaskEmail(email)
			local := email[:strings.IndexByte(email, '@')]

			if strings.Contains(masked, local) {
				t.Errorf("MaskEmail(%q) = %q leaks the local part %q", email, masked, local)
			}
			if len(masked) >= len(email) {
				t.Errorf("MaskEmail(%q) = %q is not shorter than the input", email, masked)
			}
		})
	}
}

// Masking must not be reversible by appending or stripping characters: the
// output length is independent of the input local-part length.
func TestMaskEmail_OutputLengthIsStable(t *testing.T) {
	short := MaskEmail("a@example.com")
	long := MaskEmail("abcdefghijklmnop@example.com")

	if short != long {
		t.Errorf("masked output varies with local-part length: %q vs %q", short, long)
	}
}

// A multi-byte first character must not be truncated to a single byte, which
// would emit invalid UTF-8 into the log line.
func TestMaskEmail_MultiByteFirstCharacter(t *testing.T) {
	cases := []struct {
		email string
		want  string
	}{
		{email: "ünïcödé@example.com", want: "ü***@example.com"},
		{email: "日本語@example.com", want: "日***@example.com"},
		{email: "😀😀@example.com", want: "😀***@example.com"},
		{email: "é@example.com", want: "é***@example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.email, func(t *testing.T) {
			got := MaskEmail(tc.email)
			if got != tc.want {
				t.Errorf("MaskEmail(%q) = %q, want %q", tc.email, got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("MaskEmail(%q) = %q is not valid UTF-8", tc.email, got)
			}
		})
	}
}
