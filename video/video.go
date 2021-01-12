package video

import (
	"context"

	"github.com/lbryio/transcoder/db"
)

// Library contains methods for accessing videos database.
type Library struct {
	queries Queries
}

func NewLibrary(db *db.DB) *Library {
	return &Library{queries: Queries{db}}
}

// Add records data about video into database.
func (q Library) Add(params AddParams) (*Video, error) {
	return q.queries.Add(context.Background(), params)
}

func (q Library) Get(sdHash string) (*Video, error) {
	return q.queries.Get(context.Background(), sdHash)
}
