package common

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	pb "github.com/contester/advfiler/protos"
)

type Digests struct {
	SHA1, MD5, SHA256 []byte
}

func (s *Hashes) Digests() Digests {
	return Digests{
		MD5:    s.Md5.Sum(nil),
		SHA1:   s.Sha1.Sum(nil),
		SHA256: s.Sha256.Sum(nil),
	}
}

func NewHashes() *Hashes {
	return &Hashes{
		Sha1:   sha1.New(),
		Md5:    md5.New(),
		Sha256: sha256.New(),
	}
}

func DigestsFromProto(s *pb.Digests) Digests {
	return Digests{
		MD5:    s.GetMd5(),
		SHA1:   s.GetSha1(),
		SHA256: s.GetSha256(),
	}
}

func maybeSetDigest(m map[string]string, name string, value []byte) {
	if len(value) > 0 {
		m[name] = base64.StdEncoding.EncodeToString(value)
	}
}

func DigestsToMap(d Digests) map[string]string {
	r := make(map[string]string)
	maybeSetDigest(r, "MD5", d.MD5)
	maybeSetDigest(r, "SHA", d.SHA1)
	maybeSetDigest(r, "SHA-256", d.SHA256)
	if len(r) == 0 {
		return nil
	}
	return r
}

func maybeGetDigest(x string) []byte {
	r, _ := base64.StdEncoding.DecodeString(x)
	return r
}

func MapToDigests(m map[string]string) Digests {
	return Digests{
		MD5:    maybeGetDigest(m["MD5"]),
		SHA1:   maybeGetDigest(m["SHA"]),
		SHA256: maybeGetDigest(m["SHA-256"]),
	}
}

var ErrNotFound = errors.New("not found")

func maybeCompareHash(calculated, received []byte) bool {
	if len(received) == 0 {
		return true
	}
	return bytes.Equal(calculated, received)
}

func MaybeCompareHashes(calculated, received Digests) error {
	if !maybeCompareHash(calculated.MD5, received.MD5) {
		return fmt.Errorf("md5 mismatch")
	}
	if !maybeCompareHash(calculated.SHA1, received.SHA1) {
		return fmt.Errorf("sha1 mismatch")
	}
	if !maybeCompareHash(calculated.SHA256, received.SHA256) {
		return fmt.Errorf("sha256 mismatch")
	}
	return nil
}
