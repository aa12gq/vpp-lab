package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"vpp-lab/internal/api"
	"vpp-lab/internal/config"
	"vpp-lab/internal/model"
	vppmqtt "vpp-lab/internal/mqtt"
	"vpp-lab/internal/repository"
	"vpp-lab/internal/scheduler"
	"vpp-lab/internal/state"
	"vpp-lab/internal/timeseries"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	store, err := newStateStore(ctx, cfg)
	if err != nil {
		log.Fatalf("init state store: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("close state store: %v", err)
		}
	}()

	devRepo, err := repository.NewDeviceRepository(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("init postgres: %v", err)
	}
	defer devRepo.Close()

	if err := loadOrSeedDevices(ctx, cfg.SiteID, devRepo, store); err != nil {
		log.Fatalf("load devices: %v", err)
	}
	if err := loadRecentCommands(ctx, devRepo, store); err != nil {
		log.Fatalf("load commands: %v", err)
	}

	ts := timeseries.NewWriter(cfg.InfluxURL, cfg.InfluxToken, cfg.InfluxOrg, cfg.InfluxBucket)
	defer ts.Close()

	mqttClient := vppmqtt.NewClient(cfg, store, ts).WithCommandRecorder(devRepo)
	if err := mqttClient.Connect(ctx); err != nil {
		log.Fatalf("connect mqtt: %v", err)
	}

	policy := model.Policy{
		BatteryMinSOC:     cfg.BatteryMinSOC,
		BatteryMaxSOC:     cfg.BatteryMaxSOC,
		LoadShedThreshold: cfg.LoadShedThreshold,
	}
	sch := scheduler.New(cfg.SiteID, store, mqttClient, policy)
	go sch.Run(ctx, cfg.SchedulerInterval)

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.New(cfg.SiteID, store, sch, mqttClient, devRepo, healthChecks(store, mqttClient, devRepo)...).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("vpp-lab api listening on %s site=%s", cfg.HTTPAddr, cfg.SiteID)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func newStateStore(ctx context.Context, cfg config.Config) (*state.Store, error) {
	if cfg.RedisAddr == "" {
		return state.NewStore(), nil
	}
	store, err := state.NewRedisStore(ctx, cfg.SiteID, state.RedisOptions{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("redis state store enabled addr=%s db=%d", cfg.RedisAddr, cfg.RedisDB)
	return store, nil
}

func healthChecks(store *state.Store, mqttClient *vppmqtt.Client, devRepo *repository.DeviceRepository) []api.HealthCheck {
	return []api.HealthCheck{
		{
			Name: "mqtt",
			Check: func(context.Context) error {
				return mqttClient.Healthy()
			},
		},
		{Name: "postgres", Check: devRepo.Ping},
		{Name: "state", Check: store.Ping},
	}
}

func loadRecentCommands(ctx context.Context, repo *repository.DeviceRepository, store *state.Store) error {
	commands, err := repo.ListCommands(ctx, 200)
	if err != nil {
		return err
	}
	store.SetCommands(commands)
	return nil
}

func loadOrSeedDevices(ctx context.Context, siteID string, repo *repository.DeviceRepository, store *state.Store) error {
	devices, err := repo.List(ctx)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		devices = []model.Device{
			{ID: "pv_01", SiteID: siteID, Type: model.DevicePV, Name: "PV Simulator", RatedPowerW: 100},
			{ID: "battery_01", SiteID: siteID, Type: model.DeviceBattery, Name: "Battery Pack", RatedPowerW: 80, CapacityWh: 150},
			{ID: "load_01", SiteID: siteID, Type: model.DeviceLoad, Name: "Critical Load", RatedPowerW: 40, CriticalLoad: true},
			{ID: "load_02", SiteID: siteID, Type: model.DeviceLoad, Name: "Flexible Load", RatedPowerW: 60, CriticalLoad: false},
		}
		for _, d := range devices {
			if err := repo.Upsert(ctx, d); err != nil {
				return err
			}
		}
	}
	for _, d := range devices {
		store.UpsertDevice(d)
	}
	return nil
}
