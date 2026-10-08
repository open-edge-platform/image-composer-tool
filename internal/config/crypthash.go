package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/open-edge-platform/image-composer-tool/internal/utils/shell"
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

// HashPasswordForHost turns a caller-supplied password into a SHA-512 crypt
// hash on the build host, leaving a value that is already a complete crypt(3)
// hash untouched.
//
// This exists for credentials that arrive over the API: the Web UI collects a
// plain-text password, and hashing it here means only the hash is ever written
// to a generated delta, so the plain text lives in memory and nowhere else.
// The in-chroot equivalent (imageos.hashPassword) cannot serve that purpose —
// it is unexported and runs against an install root that does not exist yet
// when a template is composed.
//
// The password is fed to openssl on stdin rather than interpolated into the
// command, so it never appears in the process table or in any command string
// the shell package logs.
func HashPasswordForHost(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("password is empty")
	}
	if IsCryptHash(password) {
		return password, nil
	}
	// -stdin consumes a single line, so an embedded newline would silently
	// truncate the password to its first line and hash the wrong value.
	if strings.ContainsAny(password, "\r\n\x00") {
		return "", fmt.Errorf("password must not contain line breaks or NUL bytes")
	}
	out, err := shell.ExecCmdWithInput(password+"\n", "openssl passwd -6 -stdin", false, shell.HostPath, nil)
	if err != nil {
		// The error from openssl can echo its input, so report only that the
		// step failed and never wrap the underlying message.
		return "", fmt.Errorf("hashing password failed")
	}
	hash := strings.TrimSpace(out)
	if !IsCryptHash(hash) {
		return "", fmt.Errorf("hashing password produced an unusable value")
	}
	return hash, nil
}
