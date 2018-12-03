package main

import (
	"context"

	pb "git.stingr.net/stingray/advfiler/protos"
)

type AuthChecker struct {
	validTokens map[string]struct{}
}

func (s *AuthChecker) Check(ctx context.Context, token string, action pb.AuthAction, path string) (bool, error) {
	if _, ok := s.validTokens[token]; ok {
		return true, nil
	}
	return false, nil
}
