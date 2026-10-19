package rpmutils

import "testing"

// testGPGPublicKey is a throwaway OpenPGP public key for tests that exercise
// code paths which validate GPG key data: placeholder bytes no longer pass.
const testGPGPublicKey = `-----BEGIN PGP PUBLIC KEY BLOCK-----

mQENBGrR3dcBCADq0OeOQGYT7Q45yCeD2z44TF4RAzinISEvmLEEpiDIvHwErbpO
ETXxhV53mqqPC+Cay9EXuKCbkrf3+mRPh9d90E9Mzj8W429X5awL8wn5O0rvXOxo
m/rAvnzJVvByPRRGYNnrepvF95K/l1QjZn3k7UXnuMP3CdmGQq8tm7lL8aLftoUu
30zjkcLrjwZVeOpN25OZbGtqrpgoESDlwD3K0RCLt2ByfNDpzydI4cA8Blayr8m1
41w8nFe85RePVXhZIjgQgvsatULKlFVt0wHOSy4rXz08WCwncwF7SHmpqYTbTeKx
xCDOzCM8HKBEwO5p1awXlQMNo05Zs94Twx4LABEBAAG0G1Rlc3QgS2V5IDx0ZXN0
QGV4YW1wbGUuY29tPokBVwQTAQoAQRYhBE1TFzCtj6Omk/S9Xoo1RHbnrfGlBQJq
0d3XAhsDBQkAAVGABQsJCAcCAiICBhUKCQgLAgQWAgMBAh4HAheAAAoJEIo1RHbn
rfGlCrsH/23SO6uq1s+jetQCanQZWbx/sn2J86ziq9EPyv50sqyiCFV//of7c+Gy
v31ysCQrKWbgbb6cx/OXZW9Bc2+UxOel18gkQOEd9CArkrNK2BM9IolKUu4SEM54
t9UPMv60q9SQHow1JAV5ywJzOXHlt+3M+BVLjqea/a8NiZ2VUipgl4MV6nGY3GiG
9+OMlSiYpbmG3mAgjJPjX+sm8gES14NxZZb6IPO1oC7zkFmui/6/pjvbo/ZFwOcF
PmpA3/f8d1A2aXPeok0h1ow5adOxRq4p0CWb9DVo6vFUlYfWPVSQXJ6g4bqYDkTx
/NFv8LTmiUZyg8FDGZSkjI7GuLzVbN4=
=xoVT
-----END PGP PUBLIC KEY BLOCK-----
`

// TestGPGPublicKey exposes testGPGPublicKey to the external rpmutils_test package.
const TestGPGPublicKey = testGPGPublicKey

// UseTempCacheDir gives the test its own cache directory, and so its own GPG key
// cache (kept under it), restoring the global configuration afterwards. It
// exposes setRPMMetadataTestCache to the external rpmutils_test package, so
// every test isolates the cache the same way.
func UseTempCacheDir(t *testing.T) string {
	t.Helper()
	return setRPMMetadataTestCache(t)
}
