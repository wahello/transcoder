package video

import (
	"context"
	"time"

	"github.com/lbryio/transcoder/db"
	"github.com/lbryio/transcoder/storage"
)

type Config struct {
	db     *db.DB
	local  storage.LocalDriver
	remote storage.RemoteDriver

	maxLocalSize  uint64
	maxRemoteSize uint64
}

func Configure() *Config {
	return &Config{}
}

// DB ...
func (c *Config) DB(db *db.DB) *Config {
	c.db = db
	return c
}

// LocalStorage ...
func (c *Config) LocalStorage(s storage.LocalDriver) *Config {
	c.local = s
	return c
}

// RemoteStorage ...
func (c *Config) RemoteStorage(s storage.RemoteDriver) *Config {
	c.remote = s
	return c
}

// MaxLocalSize ...
func (c *Config) MaxLocalSize(s string) *Config {
	c.maxLocalSize = StringToSize(s)
	return c
}

// MaxRemoteSize ...
func (c *Config) MaxRemoteSize(s string) *Config {
	c.maxRemoteSize = StringToSize(s)
	return c
}

// Library contains methods for accessing videos database.
type Library struct {
	*Config
	queries Queries
	sweeper *sweeper
}

func NewLibrary(cfg *Config) *Library {
	l := &Library{
		Config:  cfg,
		queries: Queries{cfg.db},
		sweeper: NewSweeper(),
	}
	return l
}

func (q Library) IncViews(uri, sdHash string) {
	q.sweeper.Inc(uri, sdHash)
}

// Add records data about video into database.
func (q Library) Add(params AddParams) (*Video, error) {
	return q.queries.Add(context.Background(), params)
}

func (q Library) Get(sdHash string) (*Video, error) {
	return q.queries.Get(context.Background(), sdHash)
}

func (q Library) Furlough(v *Video) error {
	ll := logger.With("sd_hash", v.SDHash)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := q.local.Delete(v.SDHash)
	if err != nil {
		ll.Warnw("failed to delete local video", "err", err)
		return err
	}

	err = q.queries.UpdatePath(ctx, v.SDHash, "")
	if err != nil {
		ll.Warnw("failed to mark video as deleted locally", "err", err)
		return err
	}

	ll.Infow("video furloughed", "url", v.URL, "size", v.GetSize(), "age", v.CreatedAt, "last_accessed", v.LastAccessed)
	return nil
}

func (q Library) Retire(v *Video) error {
	ll := logger.With("sd_hash", v.SDHash)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := q.remote.Delete(v.SDHash)
	if err != nil {
		ll.Warnw("failed to delete remote video", "err", err)
		return err
	}

	err = q.queries.Delete(ctx, v.SDHash)
	if err != nil {
		ll.Warnw("failed to delete video record", "err", err)
		return err
	}

	ll.Infow("video retired", "url", v.URL, "size", v.GetSize(), "age", v.CreatedAt, "last_accessed", v.LastAccessed)
	return nil
}

func (q Library) ListLocalOnly() ([]*Video, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return q.queries.ListLocalOnly(ctx)
}

func (q Library) ListLocal() ([]*Video, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return q.queries.ListLocal(ctx)
}

func (q Library) ListRemoteOnly() ([]*Video, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return q.queries.ListRemoteOnly(ctx)
}

func (q Library) UpdateRemotePath(sdHash, url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return q.queries.UpdateRemotePath(ctx, sdHash, url)
}
