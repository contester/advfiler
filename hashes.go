package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"net/http"
	"strings"

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

// DigestsToMap renders digests as a base64 string map for the JSON upload
// response body (distinct from the HTTP Digest header written by AddDigests).
func DigestsToMap(d Digests) map[string]string {
	r := make(map[string]string)
	for _, v := range d.lstable() {
		if len(*v.value) > 0 {
			r[strings.ToUpper(v.name)] = base64.StdEncoding.EncodeToString(*v.value)
		}
	}
	if len(r) == 0 {
		return nil
	}
	return r
}

type digestField struct {
	name  string
	value *[]byte
}

// lstable maps each digest to its RFC 3230 / RFC 5843 token name. The pointers
// allow ParseDigests to write decoded bytes directly into the struct.
func (d *Digests) lstable() []digestField {
	return []digestField{
		{"md5", &d.MD5},
		{"sha", &d.SHA1},
		{"sha-256", &d.SHA256},
		{"blake3", &d.Blake3},
	}
}

// AddDigests writes the Digest header (comma-separated token=base64 pairs) plus
// a Content-<TOKEN> header for each present digest.
func AddDigests(h http.Header, d Digests) {
	var digests []string
	for _, v := range d.lstable() {
		if len(*v.value) == 0 {
			continue
		}
		hval := base64.StdEncoding.EncodeToString(*v.value)
		digests = append(digests, v.name+"="+hval)
		h.Add("Content-"+strings.ToUpper(v.name), hval)
	}
	if len(digests) != 0 {
		h.Add("Digest", strings.Join(digests, ","))
	}
}

// ParseDigests reads the Digest header (and Content-MD5 fallback), base64-decoding
// each value into the returned Digests. Token matching is case-insensitive.
func ParseDigests(h http.Header) (result Digests) {
	if dh := h.Get("Digest"); dh != "" {
		for _, v := range strings.Split(dh, ",") {
			ds := strings.SplitN(strings.TrimSpace(v), "=", 2)
			if len(ds) != 2 {
				continue
			}
			for _, x := range result.lstable() {
				if strings.EqualFold(ds[0], x.name) {
					if nv, err := base64.StdEncoding.DecodeString(ds[1]); err == nil {
						*x.value = nv
					}
					break
				}
			}
		}
	}
	if len(result.MD5) == 0 {
		if md5d := h.Get("Content-MD5"); md5d != "" {
			result.MD5, _ = base64.StdEncoding.DecodeString(md5d)
		}
	}
	return result
}

// VerifyDigests checks each non-empty received digest against the computed one.
func VerifyDigests(computed, received Digests) error {
	pairs := []struct {
		name       string
		comp, recv []byte
	}{
		{"md5", computed.MD5, received.MD5},
		{"sha1", computed.SHA1, received.SHA1},
		{"sha256", computed.SHA256, received.SHA256},
		{"blake3", computed.Blake3, received.Blake3},
	}
	for _, p := range pairs {
		if len(p.recv) == 0 {
			continue
		}
		if !bytes.Equal(p.comp, p.recv) {
			return fmt.Errorf("%s digest mismatch: transit corruption detected", p.name)
		}
	}
	return nil
}
