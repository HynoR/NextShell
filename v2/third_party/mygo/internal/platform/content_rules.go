package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// contentRulesHash is the length of the hash ContentRulesIdentifier puts
// after the id, in hexadecimal digits.
const contentRulesHash = 16

// ContentRulesIdentifier names the list compiled from rules for id: the id
// and a hash of the rules, so that a later launch with the same rules
// loads what it compiled before, and changed rules compile again.
func ContentRulesIdentifier(id, rules string) string {
	sum := sha256.Sum256([]byte(rules))
	return id + "-" + hex.EncodeToString(sum[:])[:contentRulesHash]
}

// StaleContentRules reports whether identifier names a list compiled for
// id from other rules than current, which is ContentRulesIdentifier's.
func StaleContentRules(id, current, identifier string) bool {
	hash, ok := strings.CutPrefix(identifier, id+"-")
	if !ok || identifier == current || len(hash) != contentRulesHash {
		return false
	}
	for _, c := range []byte(hash) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
