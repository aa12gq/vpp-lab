package optimizer

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"vpp-lab/internal/model"
)

const slotDuration = 15 * time.Minute

type TariffBand struct {
	Name      string  `json:"name"`
	StartHour int     `json:"start_hour"`
	EndHour   int     `json:"end_hour"`
	Price     float64 `json:"price"`
}

type Config struct {
	HorizonHours       int          `json:"horizon_hours"`
	SlotMinutes        int          `json:"slot_minutes"`
	BatteryCapacityWh  float64      `json:"battery_capacity_wh"`
	BatteryPowerLimitW float64      `json:"battery_power_limit_w"`
	MinSOC             float64      `json:"min_soc"`
	MaxSOC             float64      `json:"max_soc"`
	Tariffs            []TariffBand `json:"tariffs"`
	minSOCSet          bool
	maxSOCSet          bool
}

type Slot struct {
	StartAt       time.Time `json:"start_at"`
	EndAt         time.Time `json:"end_at"`
	BatteryMode   string    `json:"battery_mode"`
	TargetPowerW  float64   `json:"target_power_w"`
	ExpectedPrice float64   `json:"expected_price"`
	ForecastPVW   float64   `json:"forecast_pv_w"`
	ForecastLoadW float64   `json:"forecast_load_w"`
	ForecastSOC   float64   `json:"forecast_soc"`
	NetLoadW      float64   `json:"net_load_w"`
	Reason        string    `json:"reason"`
}

type Plan struct {
	GeneratedAt time.Time `json:"generated_at"`
	SiteID      string    `json:"site_id"`
	Config      Config    `json:"config"`
	Slots       []Slot    `json:"slots"`
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type configAlias Config
	var raw struct {
		*configAlias
		MinSOC *float64 `json:"min_soc"`
		MaxSOC *float64 `json:"max_soc"`
	}
	raw.configAlias = (*configAlias)(c)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.MinSOC != nil {
		c.MinSOC = *raw.MinSOC
		c.minSOCSet = true
	}
	if raw.MaxSOC != nil {
		c.MaxSOC = *raw.MaxSOC
		c.maxSOCSet = true
	}
	return nil
}

func CurrentSlot(plan Plan, at time.Time) (Slot, bool) {
	for _, slot := range plan.Slots {
		if (at.Equal(slot.StartAt) || at.After(slot.StartAt)) && at.Before(slot.EndAt) {
			return slot, true
		}
	}
	return Slot{}, false
}

func DefaultConfig() Config {
	return Config{
		HorizonHours:       24,
		SlotMinutes:        15,
		BatteryCapacityWh:  150,
		BatteryPowerLimitW: 50,
		MinSOC:             0.25,
		MaxSOC:             0.90,
		Tariffs: []TariffBand{
			{Name: "valley", StartHour: 0, EndHour: 7, Price: 0.32},
			{Name: "flat", StartHour: 7, EndHour: 18, Price: 0.58},
			{Name: "peak", StartHour: 18, EndHour: 23, Price: 0.95},
			{Name: "flat", StartHour: 23, EndHour: 24, Price: 0.58},
		},
	}
}

func BuildDayAheadPlan(now time.Time, summary model.SiteSummary, cfg Config) Plan {
	cfg = normalizeConfig(cfg)
	start := now.Truncate(slotDuration)
	slotCount := cfg.HorizonHours * 60 / cfg.SlotMinutes
	if slotCount <= 0 {
		slotCount = 96
	}

	soc := summary.AvgSOC
	if soc == 0 {
		soc = 0.5
	}
	soc = clamp(soc, cfg.MinSOC, cfg.MaxSOC)
	currentPV := math.Max(summary.PVPowerW, 20)
	currentLoad := math.Max(summary.LoadPowerW, 30)

	plan := Plan{
		GeneratedAt: now,
		SiteID:      summary.SiteID,
		Config:      cfg,
		Slots:       make([]Slot, 0, slotCount),
	}
	for i := 0; i < slotCount; i++ {
		slotStart := start.Add(time.Duration(i*cfg.SlotMinutes) * time.Minute)
		price := priceAt(slotStart, cfg.Tariffs)
		low := lowPrice(cfg.Tariffs)
		high := highPrice(cfg.Tariffs)
		hasArbitrage := high > low
		pv := forecastPV(slotStart, currentPV)
		load := forecastLoad(slotStart, currentLoad)
		netLoad := load - pv

		mode := "idle"
		target := 0.0
		reason := "balanced or battery held for constraints"

		switch {
		case pv > load+10 && soc < cfg.MaxSOC:
			mode = "charge"
			target = math.Min(cfg.BatteryPowerLimitW, pv-load)
			reason = "pv surplus"
		case hasArbitrage && price <= low && soc < cfg.MaxSOC:
			mode = "charge"
			target = cfg.BatteryPowerLimitW
			reason = "low tariff"
		case hasArbitrage && price >= high && netLoad > 0 && soc > cfg.MinSOC:
			mode = "discharge"
			target = math.Min(cfg.BatteryPowerLimitW, netLoad)
			reason = "peak tariff and load deficit"
		case netLoad > cfg.BatteryPowerLimitW && soc > cfg.MinSOC:
			mode = "discharge"
			target = math.Min(cfg.BatteryPowerLimitW, netLoad)
			reason = "reduce net load"
		}

		soc = nextSOC(soc, mode, target, cfg)
		plan.Slots = append(plan.Slots, Slot{
			StartAt:       slotStart,
			EndAt:         slotStart.Add(time.Duration(cfg.SlotMinutes) * time.Minute),
			BatteryMode:   mode,
			TargetPowerW:  round2(target),
			ExpectedPrice: price,
			ForecastPVW:   round2(pv),
			ForecastLoadW: round2(load),
			ForecastSOC:   round4(soc),
			NetLoadW:      round2(netLoad),
			Reason:        reason,
		})
	}
	return plan
}

func ValidateConfig(cfg Config) error {
	if cfg.HorizonHours < 0 {
		return fmt.Errorf("horizon_hours must be > 0")
	}
	if cfg.SlotMinutes < 0 {
		return fmt.Errorf("slot_minutes must be > 0")
	}
	if cfg.SlotMinutes > 0 && 60%cfg.SlotMinutes != 0 {
		return fmt.Errorf("slot_minutes must divide 60")
	}
	if cfg.BatteryCapacityWh < 0 {
		return fmt.Errorf("battery_capacity_wh must be > 0")
	}
	if cfg.BatteryPowerLimitW < 0 {
		return fmt.Errorf("battery_power_limit_w must be > 0")
	}
	if cfg.MinSOC < 0 || cfg.MinSOC > 1 {
		return fmt.Errorf("min_soc must be between 0 and 1")
	}
	if cfg.MaxSOC < 0 || cfg.MaxSOC > 1 {
		return fmt.Errorf("max_soc must be between 0 and 1")
	}
	if cfg.MinSOC > cfg.MaxSOC {
		return fmt.Errorf("min_soc must be <= max_soc")
	}
	normalized := normalizeConfig(cfg)
	for i, band := range cfg.Tariffs {
		if band.StartHour < 0 || band.StartHour > 24 || band.EndHour < 0 || band.EndHour > 24 {
			return fmt.Errorf("tariffs[%d] hours must be between 0 and 24", i)
		}
		if band.StartHour >= band.EndHour {
			return fmt.Errorf("tariffs[%d] start_hour must be < end_hour", i)
		}
		if band.Price < 0 {
			return fmt.Errorf("tariffs[%d] price must be >= 0", i)
		}
	}
	if normalized.HorizonHours <= 0 {
		return fmt.Errorf("horizon_hours must be > 0")
	}
	if normalized.SlotMinutes <= 0 {
		return fmt.Errorf("slot_minutes must be > 0")
	}
	if normalized.BatteryCapacityWh <= 0 {
		return fmt.Errorf("battery_capacity_wh must be > 0")
	}
	if normalized.BatteryPowerLimitW <= 0 {
		return fmt.Errorf("battery_power_limit_w must be > 0")
	}
	if normalized.MinSOC > normalized.MaxSOC {
		return fmt.Errorf("min_soc must be <= max_soc")
	}
	return nil
}

func normalizeConfig(cfg Config) Config {
	def := DefaultConfig()
	if cfg.HorizonHours == 0 {
		cfg.HorizonHours = def.HorizonHours
	}
	if cfg.SlotMinutes == 0 {
		cfg.SlotMinutes = def.SlotMinutes
	}
	if cfg.BatteryCapacityWh == 0 {
		cfg.BatteryCapacityWh = def.BatteryCapacityWh
	}
	if cfg.BatteryPowerLimitW == 0 {
		cfg.BatteryPowerLimitW = def.BatteryPowerLimitW
	}
	if cfg.MinSOC == 0 && !cfg.minSOCSet {
		cfg.MinSOC = def.MinSOC
	}
	if cfg.MaxSOC == 0 && !cfg.maxSOCSet {
		cfg.MaxSOC = def.MaxSOC
	}
	if len(cfg.Tariffs) == 0 {
		cfg.Tariffs = def.Tariffs
	}
	return cfg
}

func forecastPV(t time.Time, currentPV float64) float64 {
	hour := float64(t.Hour()) + float64(t.Minute())/60
	shape := math.Sin((hour - 6) / 12 * math.Pi)
	if shape < 0 {
		return 0
	}
	return currentPV * shape
}

func forecastLoad(t time.Time, currentLoad float64) float64 {
	hour := t.Hour()
	multiplier := 1.0
	switch {
	case hour >= 7 && hour < 10:
		multiplier = 1.15
	case hour >= 18 && hour < 23:
		multiplier = 1.35
	case hour >= 0 && hour < 6:
		multiplier = 0.75
	}
	return currentLoad * multiplier
}

func priceAt(t time.Time, bands []TariffBand) float64 {
	h := t.Hour()
	for _, band := range bands {
		if h >= band.StartHour && h < band.EndHour {
			return band.Price
		}
	}
	return 0.58
}

func lowPrice(bands []TariffBand) float64 {
	min := bands[0].Price
	for _, band := range bands {
		if band.Price < min {
			min = band.Price
		}
	}
	return min
}

func highPrice(bands []TariffBand) float64 {
	max := bands[0].Price
	for _, band := range bands {
		if band.Price > max {
			max = band.Price
		}
	}
	return max
}

func nextSOC(soc float64, mode string, powerW float64, cfg Config) float64 {
	delta := powerW * float64(cfg.SlotMinutes) / 60 / cfg.BatteryCapacityWh
	switch mode {
	case "charge":
		soc += delta
	case "discharge":
		soc -= delta
	}
	return clamp(soc, cfg.MinSOC, cfg.MaxSOC)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func round4(v float64) float64 {
	return math.Round(v*10000) / 10000
}
