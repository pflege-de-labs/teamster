package store

import (
	"context"
	"fmt"
)

func (s queryAdapter) AuthzGeneration(ctx context.Context) (int64, error) {
	generation, err := s.q.GetAuthzGeneration(ctx)
	if err != nil {
		return 0, fmt.Errorf("read authz generation: %w", err)
	}
	return generation, nil
}

func (s queryAdapter) BumpAuthzGeneration(ctx context.Context) error {
	if err := s.q.BumpAuthzGeneration(ctx); err != nil {
		return fmt.Errorf("bump authz generation: %w", err)
	}
	return nil
}
