package credentialstore

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("credentialstore: not found")

type Store interface {
	Get(context.Context, string, string) ([]byte, error)
	Set(context.Context, string, string, []byte) error
	Delete(context.Context, string, string) error
}
