package main

import (
	"math/rand"
	"os"
	"os/signal"
	"path"
	"runtime/pprof"
	"strconv"
	"syscall"
	"time"

	"github.com/lbryio/transcoder/api"
	"github.com/lbryio/transcoder/db"
	"github.com/lbryio/transcoder/encoder"
	"github.com/lbryio/transcoder/formats"
	"github.com/lbryio/transcoder/pkg/claim"
	"github.com/lbryio/transcoder/pkg/config"
	"github.com/lbryio/transcoder/pkg/logging"
	"github.com/lbryio/transcoder/queue"
	"github.com/lbryio/transcoder/storage"
	"github.com/lbryio/transcoder/video"
	"github.com/pkg/profile"

	"github.com/alecthomas/kong"
)

var logger = logging.Create("main", logging.Dev)

var CLI struct {
	Serve struct {
		Bind         string `optional name:"bind" help:"Address to listen on." default:":8080"`
		DataPath     string `optional name:"data-path" help:"Path to store database files and configs." type:"existingdir" default:"."`
		VideoPath    string `optional name:"video-path" help:"Path to store video." type:"existingdir" default:"."`
		Workers      int    `optional name:"workers" help:"Number of workers to start." type:"int" default:"10"`
		CDN          string `optional name:"cdn" help:"LBRY CDN endpoint address."`
		Debug        bool   `optional name:"debug" help:"Debug mode."`
		ProfileCPU   bool   `optional name:"profile-cpu" help:"Enable CPU profiling."`
		ProfileTrace bool   `optional name:"profile-trace" help:"Enable execution tracer."`
	} `cmd help:"Start transcoding server."`
}

const cpuPF = "cpu.pprof"

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	cfg, err := config.Read()
	cfg.SetDefault("CDNServer", "https://cdn.lbryplayer.xyz/api/v3/streams")
	if err != nil {
		logger.Fatal(err)
	}

	ctx := kong.Parse(&CLI)
	switch ctx.Command() {
	case "serve":
		if CLI.Serve.ProfileCPU {
			logger.Infof("outputting CPU profile to %v", cpuPF)
			f, err := os.Create(cpuPF)
			if err != nil {
				logger.Fatal("could not create CPU profile: ", err)
			}
			defer f.Close()
			if err := pprof.StartCPUProfile(f); err != nil {
				logger.Fatal("could not start CPU profiling: ", err)
			}
			defer pprof.StopCPUProfile()
		}
		if CLI.Serve.ProfileTrace {
			logger.Info("tracing enabled")
			defer profile.Start(profile.TraceProfile, profile.ProfilePath(".")).Stop()
		}

		if !CLI.Serve.Debug {
			api.SetLogger(logging.Create("api", logging.Prod))
			db.SetLogger(logging.Create("db", logging.Prod))
			queue.SetLogger(logging.Create("queue", logging.Prod))
			encoder.SetLogger(logging.Create("encoder", logging.Prod))
			video.SetLogger(logging.Create("video", logging.Prod))
			claim.SetLogger(logging.Create("claim", logging.Prod))
			storage.SetLogger(logging.Create("storage", logging.Prod))
			formats.SetLogger(logging.Create("formats", logging.Prod))
		}

		if CLI.Serve.CDN != "" {
			claim.SetCDNServer(CLI.Serve.CDN)
		} else {
			claim.SetCDNServer(cfg.GetString("CDNServer"))
		}

		vdb := db.OpenDB(path.Join(CLI.Serve.DataPath, "video.sqlite"))
		err := vdb.MigrateUp(video.InitialMigration)
		if err != nil {
			logger.Fatal(err)
		}

		qdb := db.OpenDB(path.Join(CLI.Serve.DataPath, "queue.sqlite"))
		err = qdb.MigrateUp(queue.InitialMigration)
		if err != nil {
			logger.Fatal(err)
		}

		wasabi := cfg.GetStringMapString("wasabi")
		local := cfg.GetStringMapString("local")

		libCfg := video.Configure().
			LocalStorage(storage.Local(CLI.Serve.VideoPath)).
			MaxLocalSize(local["maxsize"]).
			MaxRemoteSize(wasabi["maxsize"]).
			DB(vdb)

		if wasabi["bucket"] != "" {
			s3d, err := storage.InitS3Driver(
				storage.S3ConfigureWasabiEU().
					Credentials(wasabi["key"], wasabi["secret"]).
					Bucket(wasabi["bucket"]))
			if err != nil {
				logger.Fatalw("wasabi driver initialization failed", "err", err)
			}
			libCfg.RemoteStorage(s3d)
			logger.Infow("wasabi storage configured", "bucket", wasabi["bucket"])
		}
		lib := video.NewLibrary(libCfg)

		if wasabi["bucket"] != "" {
			video.SpawnS3Uploader(lib)
		}

		q := queue.NewQueue(qdb)

		video.LoadEnabledChannels(cfg.GetStringSlice("enabledchannels"))

		poller := q.StartPoller(CLI.Serve.Workers)
		for i := 0; i < CLI.Serve.Workers; i++ {
			go video.SpawnProcessing(q, lib, poller)
		}

		video.SpawnLibraryCleaning(lib)
		sweeperCfg := cfg.GetStringMapString("sweeper")
		if sweeperCfg != nil {
			interval, err := strconv.Atoi(sweeperCfg["intervalminutes"])
			if err != nil {
				interval = 10
				logger.Warnf("invalid sweeper interval: %v, setting to 10 min", sweeperCfg["intervalminutes"])
			}
			lowerBound, _ := strconv.Atoi(sweeperCfg["lowerbound"])
			topNumber, _ := strconv.Atoi(sweeperCfg["topnumber"])
			video.SpawnPopularSweeper(lib, q, video.PopularSweeperOpts{
				Interval:   time.Duration(interval) * time.Minute,
				LowerBound: lowerBound,
				TopNumber:  topNumber,
			})
		}

		apiServer := api.NewServer(
			api.Configure().
				Debug(CLI.Serve.Debug).
				Addr(CLI.Serve.Bind).
				VideoPath(CLI.Serve.VideoPath).
				VideoManager(api.NewManager(q, lib)),
		)
		logger.Infow("configured api server", "addr", CLI.Serve.Bind)

		go func() {
			err = apiServer.Start()
			if err != nil {
				logger.Fatal(err)
			}
		}()

		stopChan := make(chan os.Signal, 1)
		signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
		sig := <-stopChan
		logger.Infof("caught an %v signal, shutting down", sig)
		apiServer.Shutdown()
		poller.Shutdown()
	default:
		logger.Fatal(ctx.Command())
	}
}
