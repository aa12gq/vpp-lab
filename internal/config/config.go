package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr          string
	SiteID            string
	MQTTBroker        string
	MQTTClientID      string
	MQTTUsername      string
	MQTTPassword      string
	MQTTTLSCAFile     string
	MQTTTLSCertFile   string
	MQTTTLSKeyFile    string
	MQTTTLSInsecure   bool
	InfluxURL         string
	InfluxToken       string
	InfluxOrg         string
	InfluxBucket      string
	PostgresDSN       string
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	ControlToken      string
	SchedulerInterval time.Duration
	BatteryMinSOC     float64
	BatteryMaxSOC     float64
	LoadShedThreshold float64
}

func Load() Config {
	return Config{
		HTTPAddr:          getenv("HTTP_ADDR", ":8080"),
		SiteID:            getenv("SITE_ID", "home-lab"),
		MQTTBroker:        getenv("MQTT_BROKER", "tcp://localhost:1883"),
		MQTTClientID:      getenv("MQTT_CLIENT_ID", "vpp-platform"),
		MQTTUsername:      getenv("MQTT_USERNAME", ""),
		MQTTPassword:      getenv("MQTT_PASSWORD", ""),
		MQTTTLSCAFile:     getenv("MQTT_TLS_CA_FILE", ""),
		MQTTTLSCertFile:   getenv("MQTT_TLS_CERT_FILE", ""),
		MQTTTLSKeyFile:    getenv("MQTT_TLS_KEY_FILE", ""),
		MQTTTLSInsecure:   getbool("MQTT_TLS_INSECURE_SKIP_VERIFY", false),
		InfluxURL:         getenv("INFLUX_URL", "http://localhost:8086"),
		InfluxToken:       getenv("INFLUX_TOKEN", "vpp-lab-token"),
		InfluxOrg:         getenv("INFLUX_ORG", "vpp-lab"),
		InfluxBucket:      getenv("INFLUX_BUCKET", "vpp"),
		PostgresDSN:       getenv("POSTGRES_DSN", "postgres://vpp:vpp@localhost:5432/vpp?sslmode=disable"),
		RedisAddr:         getenv("REDIS_ADDR", ""),
		RedisPassword:     getenv("REDIS_PASSWORD", ""),
		RedisDB:           getint("REDIS_DB", 0),
		ControlToken:      getenv("CONTROL_TOKEN", ""),
		SchedulerInterval: getdur("SCHEDULER_INTERVAL", 5*time.Second),
		BatteryMinSOC:     getfloat("BATTERY_MIN_SOC", 0.25),
		BatteryMaxSOC:     getfloat("BATTERY_MAX_SOC", 0.90),
		LoadShedThreshold: getfloat("LOAD_SHED_THRESHOLD_W", 80),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getdur(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getfloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getint(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getbool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
