package storage

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"golang.org/x/crypto/ssh"
	"nextshell.dev/v2/internal/model"
)

func keyMetadata(k *model.Key) {
	pub, comment, _, _, e := ssh.ParseAuthorizedKey([]byte(k.Public))
	if e != nil {
		return
	}
	k.Fingerprint = ssh.FingerprintSHA256(pub)
	k.KeyType = pub.Type()
	k.KeyComment = comment
	if c, ok := pub.(ssh.CryptoPublicKey); ok {
		switch v := c.CryptoPublicKey().(type) {
		case *rsa.PublicKey:
			k.KeyBits = v.N.BitLen()
		case *ecdsa.PublicKey:
			k.KeyBits = v.Curve.Params().BitSize
		case ed25519.PublicKey:
			k.KeyBits = len(v) * 8
		}
	}
}
