package main

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"hash"

	pb "github.com/contester/advfiler/protos"
	"lukechampine.com/blake3"
)

type Digests struct {
	Blake3, SHA256, SHA1, MD5 []byte
}

type Hashes struct {
	blake3       hash.Hash
	sha256, sha1 hash.Hash
	md5          hash.Hash
}

func NewHashes() *Hashes {
	return &Hashes{
		blake3: blake3.New(32, nil),
		sha256: sha256.New(),
		sha1:   sha1.New(),
		md5:    md5.New(),
	}
}

func (s *Hashes) Write(p []byte) (int, error) {
	s.blake3.Write(p)
	s.sha256.Write(p)
	s.sha1.Write(p)
	return s.md5.Write(p)
}

func (s *Hashes) Digests() Digests {
	return Digests{
		Blake3: s.blake3.Sum(nil),
		SHA256: s.sha256.Sum(nil),
		SHA1:   s.sha1.Sum(nil),
		MD5:    s.md5.Sum(nil),
	}
}

func DigestsFromProto(s *pb.Digests) Digests {
	if s == nil {
		return Digests{}
	}
	return Digests{
		SHA256: s.GetSha256(),
		SHA1:   s.GetSha1(),
		MD5:    s.GetMd5(),
	}
}

func (d Digests) ToProto() *pb.Digests {
	return &pb.Digests{
		Sha256: d.SHA256,
		Sha1:   d.SHA1,
		Md5:    d.MD5,
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

func MapToDigests(m map[string]string) Digests {
	return Digests{
		MD5:    maybeGetDigest(m["MD5"]),
		SHA1:   maybeGetDigest(m["SHA"]),
		SHA256: maybeGetDigest(m["SHA-256"]),
	}
}

func maybeGetDigest(x string) []byte {
	r, _ := base64.StdEncoding.DecodeString(x)
	return r
}
