package client

import (
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/karrick/godirwalk"
	"github.com/lbryio/transcoder/api"
	"github.com/lbryio/transcoder/db"
	"github.com/lbryio/transcoder/queue"
	"github.com/lbryio/transcoder/storage"
	"github.com/lbryio/transcoder/video"
	"github.com/stretchr/testify/suite"
)

var streamURL = "lbry://@specialoperationstest#3/fear-of-death-inspirational#a"
var streamSDHash = "f12fb044f5805334a473bf9a81363d89bd1cb54c4065ac05be71a599a6c51efc6c6afb257208326af304324094105774"

type ClientSuite struct {
	suite.Suite
	assetsPath string
	apiServer  *api.APIServer
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

func (s *ClientSuite) SetupTest() {
	s.assetsPath = path.Join(os.TempDir(), "transcoder_test")
	os.RemoveAll(s.assetsPath)
	s.Require().NoError(os.MkdirAll(path.Join(s.assetsPath, "sqlite"), os.ModePerm))
	s.Require().NoError(os.MkdirAll(path.Join(s.assetsPath, "videos"), os.ModePerm))
	s.Require().NoError(os.MkdirAll(path.Join(s.assetsPath, "client"), os.ModePerm))

	vdb := db.OpenDB(path.Join(s.assetsPath, "sqlite", "video.sqlite"))
	vdb.MigrateUp(video.InitialMigration)
	qdb := db.OpenDB(path.Join(s.assetsPath, "sqlite", "queue.sqlite"))
	qdb.MigrateUp(queue.InitialMigration)

	lib := video.NewLibrary(
		video.Configure().
			LocalStorage(storage.Local(path.Join(s.assetsPath, "videos"))).
			DB(vdb),
	)
	q := queue.NewQueue(qdb)

	poller := q.StartPoller(1)
	go video.SpawnProcessing(q, lib, poller)
	s.apiServer = api.NewServer(
		api.Configure().
			Debug(true).
			Addr("127.0.0.1:50808").
			VideoPath(path.Join(s.assetsPath, "videos")).
			VideoManager(api.NewManager(q, lib)),
	)
	go s.apiServer.Start()

	video.LoadEnabledChannels(
		[]string{
			"@specialoperationstest#3",
		})
}

func (s *ClientSuite) TearDownTest() {
	go s.apiServer.Shutdown()
	s.Require().NoError(os.RemoveAll(s.assetsPath))
}

func (s *ClientSuite) TestRestoreCache() {
	dstPath := path.Join(s.assetsPath, "TestRestoreCache")

	c := New(Configure().VideoPath(dstPath))

	cvDirs := []string{}
	for range [10]int{} {
		sdHash := randomString(96)
		s.populateHLSPlaylist(dstPath, sdHash)
		cvDirs = append(cvDirs, sdHash)
	}

	c = New(Configure().VideoPath(dstPath))
	n, err := c.RestoreCache()
	s.Require().NoError(err)
	s.EqualValues((78*4+5)*10, n)

	for _, sdHash := range cvDirs {
		fragments, err := godirwalk.ReadDirnames(path.Join(dstPath, sdHash), nil)
		s.Require().NoError(err)
		for _, fname := range fragments {
			fg, hit, err := c.getCachedFragment("zzz", sdHash, fname)
			s.Require().NoError(err)
			s.Require().NotNil(fg)
			s.Require().True(hit)

			fi, err := os.Stat(c.fullFragmentPath(fg))
			s.Require().NoError(err)
			s.EqualValues(fi.Size(), fg.Size())
		}
	}
}

func (s *ClientSuite) Test_sdHashRe() {
	m := sdHashRe.FindStringSubmatch("http://t0.lbry.tv:18081/streams/85e8ad21f40550ebf0f30f7a0f6f092e8c62c7c697138e977087ac7b7f29554f8e0270447922493ff564457b60f45b18/master.m3u8")
	s.Equal("85e8ad21f40550ebf0f30f7a0f6f092e8c62c7c697138e977087ac7b7f29554f8e0270447922493ff564457b60f45b18", m[1])
}

func (s *ClientSuite) Test_fragmentURL() {
	dstPath := path.Join(s.assetsPath, "Test_fragmentURL")
	c := New(Configure().Server("http://t0.lbry.tv:18081").VideoPath(dstPath).LogLevel(Dev))

	u, err := c.fragmentURL("morgan", "0b8dfc049b2165fad5829aca24f2ddfae3acef8d73bc5e04ff8b932fce9fc463dc6cf3e638413f04536638d2e7218427", "master.m3u8")
	s.Require().Error(err)
	s.Regexp("remote sd hash mismatch", err.Error())
	s.Equal("", u)

	u, err = c.fragmentURL("morgan", "azazaz", "master.m3u8")
	s.Require().Error(err)
	s.Regexp("remote sd hash mismatch", err.Error())
	s.Equal("", u)

	u, err = c.fragmentURL("vanquish-trailer-(2021)-morgan-freeman,#b7b150d1bbca4650ad4ab921dd8d424bf77c1141", "azazaz", "master.m3u8")
	s.Require().Error(err)
	s.Regexp("remote sd hash mismatch", err.Error())
	s.Equal("", u)

	u, err = c.fragmentURL(
		"vanquish-trailer-(2021)-morgan-freeman,#b7b150d1bbca4650ad4ab921dd8d424bf77c1141",
		"bec50ab288153ed03b0eb8dafd814daf19a187e07f8da4ad91cf778f5c39ac74d9d92ad6e3ebf2ddb6b7acea3cb8893a",
		"master.m3u8")
	s.Require().NoError(err)
	s.Equal("http://t0.lbry.tv:18081/streams/bec50ab288153ed03b0eb8dafd814daf19a187e07f8da4ad91cf778f5c39ac74d9d92ad6e3ebf2ddb6b7acea3cb8893a/master.m3u8", u)
}

func randomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

// populateHLSPlaylist generates a stream of 3131915 bytes in size, segments binary data will all be zeroes.
func (s *ClientSuite) populateHLSPlaylist(dstPath, sdHash string) {
	err := os.MkdirAll(path.Join(dstPath, sdHash), os.ModePerm)
	s.Require().NoError(err)

	srcPath, _ := filepath.Abs("./testdata")
	storage := storage.Local(srcPath)
	ls, err := storage.Open("dummystream")
	s.Require().NoError(err)
	err = ls.Dive(
		func(rootPath ...string) ([]byte, error) {
			if path.Ext(rootPath[len(rootPath)-1]) == ".m3u8" {
				return ioutil.ReadFile(path.Join(rootPath...))
			}
			return make([]byte, 10000), nil
		},
		func(data []byte, name string) error {
			return ioutil.WriteFile(path.Join(dstPath, sdHash, name), data, os.ModePerm)
		},
	)
	s.Require().NoError(err)
}
