package config

import (
	"regexp"
	"strconv"
)

// cryptHashRes match complete crypt(3) hashes, not just an algorithm prefix,
// so a truncated value such as "$6$salt" is never taken for a hash:
// $1$ (md5), $5$ (sha256), $6$ (sha512), $y$/$gy$ (yescrypt), $7$ (scrypt),
// $2a/b/x/y$ (bcrypt; libxcrypt accepts $2x$; cost 04-31).
var cryptHashRes = []*regexp.Regexp{
	regexp.MustCompile(`^\$1\$[./0-9A-Za-z]{0,8}\$[./0-9A-Za-z]{22}$`),
	regexp.MustCompile(`^\$5\$(rounds=[0-9]+\$)?[./0-9A-Za-z]{0,16}\$[./0-9A-Za-z]{43}$`),
	regexp.MustCompile(`^\$6\$(rounds=[0-9]+\$)?[./0-9A-Za-z]{0,16}\$[./0-9A-Za-z]{86}$`),
	regexp.MustCompile(`^\$g?y\$[./0-9A-Za-z]+\$[./0-9A-Za-z]+\$[./0-9A-Za-z]{43}$`),
	regexp.MustCompile(`^\$7\$[./0-9A-Za-z]{11,}\$[./0-9A-Za-z]{43}$`),
	regexp.MustCompile(`^\$2[abxy]\$(0[4-9]|[12][0-9]|3[01])\$[./0-9A-Za-z]{53}$`),
}

// shaRoundsRe extracts the optional rounds parameter of a SHA-crypt hash.
var shaRoundsRe = regexp.MustCompile(`^\$[56]\$rounds=([0-9]+)\$`)

// crypt(3) accepts SHA-crypt rounds from 1000 to 999999999. A hash outside
// the range would be written to /etc/shadow but not authenticate.
const (
	minShaCryptRounds = 1000
	maxShaCryptRounds = 999999999
)

func shaCryptRoundsValid(password string) bool {
	m := shaRoundsRe.FindStringSubmatch(password)
	if m == nil {
		return true
	}
	rounds, err := strconv.ParseUint(m[1], 10, 64)
	return err == nil && rounds >= minShaCryptRounds && rounds <= maxShaCryptRounds
}

// IsCryptHash reports whether password is a complete crypt(3) hash that can
// be set as-is. Both the config merge and the account setup use it, so a
// value is never treated as a hash by one and as plaintext by the other.
func IsCryptHash(password string) bool {
	for _, re := range cryptHashRes {
		if re.MatchString(password) {
			return shaCryptRoundsValid(password)
		}
	}
	return false
}
