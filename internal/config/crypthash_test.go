package config

import (
	"strings"
	"testing"
)

var (
	testSHA512Hash = "$6$saltsalt$" + strings.Repeat("aB0./", 17) + "x"
	testSHA256Hash = "$5$saltsalt$" + strings.Repeat("aB0./", 8) + "xyz"
	testBcryptHash = "$2b$12$" + strings.Repeat("aB0./", 10) + "xyz"
)

func TestIsCryptHash(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		testSHA512Hash: true,
		"$6$rounds=5000$saltsalt$" + testSHA512Hash[len("$6$saltsalt$"):]: true,
		testSHA256Hash:                       true,
		"$1$saltsalt$0123456789abcdefghijkl": true,
		"$y$j9T$saltsalt$0123456789abcdefghijklmnopqrstuvwxyzABCDEFG": true,
		testBcryptHash: true,
		"$2x$12$" + strings.Repeat("aB0./", 10) + "xyz": true,
		"$2y$12$" + strings.Repeat("aB0./", 10) + "xyz": true,
		"$2a$12$" + strings.Repeat("aB0./", 10) + "xyz": true,
		"$2b$04$" + strings.Repeat("aB0./", 10) + "xyz": true,
		"$2b$31$" + strings.Repeat("aB0./", 10) + "xyz": true,
		// SHA-crypt rounds must be within 1000-999999999.
		"$6$rounds=1000$saltsalt$" + testSHA512Hash[len("$6$saltsalt$"):]:                 true,
		"$6$rounds=999999999$saltsalt$" + testSHA512Hash[len("$6$saltsalt$"):]:            true,
		"$6$rounds=999$saltsalt$" + testSHA512Hash[len("$6$saltsalt$"):]:                  false,
		"$6$rounds=1000000000$saltsalt$" + testSHA512Hash[len("$6$saltsalt$"):]:           false,
		"$6$rounds=0$saltsalt$" + testSHA512Hash[len("$6$saltsalt$"):]:                    false,
		"$5$rounds=99999999999999999999$saltsalt$" + testSHA256Hash[len("$5$saltsalt$"):]: false,
		"$5$rounds=999$saltsalt$" + testSHA256Hash[len("$5$saltsalt$"):]:                  false,
		// bcrypt only supports costs 04-31.
		"$2b$03$" + strings.Repeat("aB0./", 10) + "xyz": false,
		"$2b$32$" + strings.Repeat("aB0./", 10) + "xyz": false,
		"$2b$00$" + strings.Repeat("aB0./", 10) + "xyz": false,
		"$2b$99$" + strings.Repeat("aB0./", 10) + "xyz": false,
		// Salts use the crypt base64 alphabet only.
		"$6$!!!!$" + testSHA512Hash[len("$6$saltsalt$"):]:              false,
		"$5$sa-lt$" + testSHA256Hash[len("$5$saltsalt$"):]:             false,
		"$1$sa,lt$0123456789abcdefghijkl":                              false,
		"$6$rounds=5000$sa=lt$" + testSHA512Hash[len("$6$saltsalt$"):]: false,
		// Incomplete or malformed values must be treated as plaintext.
		"$6$salt":              false,
		"$6$salt$hashvalue":    false,
		"$5$salt$short":        false,
		"$2b$bogus":            false,
		"$2b$12$tooshort":      false,
		"$y$j9T$salt":          false,
		"plaintext":            false,
		"$notahash":            false,
		"$6$salt with space$h": false,
		"":                     false,
	}
	for in, want := range tests {
		if got := IsCryptHash(in); got != want {
			t.Errorf("IsCryptHash(%q) = %t, want %t", in, got, want)
		}
	}
}
