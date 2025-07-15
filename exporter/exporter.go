package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// WalgExporter implements the Prometheus Collector interface
type WalgExporter struct {
	walgPath       string
	scrapeInterval time.Duration

	// Metrics
	backupLag       *prometheus.GaugeVec
	walLag          *prometheus.GaugeVec
	lsnLag          *prometheus.GaugeVec
	pitrWindow      prometheus.Gauge
	errors          *prometheus.CounterVec
	walIntegrity    *prometheus.GaugeVec
	backupCount     *prometheus.GaugeVec
	backupTimestamp *prometheus.GaugeVec
	scrapeDuration  prometheus.Gauge
	scrapeErrors    prometheus.Counter

	// Internal state
	lastScrape time.Time
}

// BackupInfo represents backup information from backup-list --detail --json
type BackupInfo struct {
	BackupName  string    `json:"backup_name"`
	Time        time.Time `json:"time"`
	WalFileName string    `json:"wal_file_name"`
	StartLSN    string    `json:"start_lsn"`
	FinishLSN   string    `json:"finish_lsn"`
	Permanent   bool      `json:"permanent"`
	UserData    string    `json:"user_data"`
	IsFull      bool      `json:"is_full"`
}

// WalInfo represents WAL information from wal-show --detailed-json
type WalInfo struct {
	Integrity struct {
		Status  string `json:"status"`
		Details []struct {
			TimelineID    int    `json:"timeline_id"`
			StartSegment  string `json:"start_segment"`
			EndSegment    string `json:"end_segment"`
			SegmentsCount int    `json:"segments_count"`
			Status        string `json:"status"`
		} `json:"details"`
	} `json:"integrity"`
	Timeline struct {
		Status  string `json:"status"`
		Details struct {
			CurrentTimelineID        int `json:"current_timeline_id"`
			HighestStorageTimelineID int `json:"highest_storage_timeline_id"`
		} `json:"details"`
	} `json:"timeline"`
}

// NewWalgExporter creates a new WAL-G exporter
func NewWalgExporter(walgPath string, scrapeInterval time.Duration) (*WalgExporter, error) {
	return &WalgExporter{
		walgPath:       walgPath,
		scrapeInterval: scrapeInterval,

		backupLag: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "walg_backup_lag_seconds",
				Help: "Time since last backup-push in seconds",
			},
			[]string{"backup_type"},
		),

		walLag: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "walg_wal_lag_seconds",
				Help: "Time since last wal-push in seconds",
			},
			[]string{"timeline"},
		),

		lsnLag: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "walg_lsn_lag_bytes",
				Help: "LSN delta lag in bytes",
			},
			[]string{"timeline"},
		),

		pitrWindow: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "walg_pitr_window_seconds",
				Help: "Point-in-time recovery window size in seconds",
			},
		),

		errors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "walg_errors_total",
				Help: "Total number of WAL-G errors",
			},
			[]string{"operation", "error_type"},
		),

		walIntegrity: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "walg_wal_integrity_status",
				Help: "WAL integrity status (1 = OK, 0 = ERROR)",
			},
			[]string{"timeline"},
		),

		backupCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "walg_backup_count",
				Help: "Number of backups",
			},
			[]string{"backup_type"},
		),

		backupTimestamp: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "walg_backup_timestamp",
				Help: "Timestamp of last backup",
			},
			[]string{"backup_type"},
		),

		scrapeDuration: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "walg_scrape_duration_seconds",
				Help: "Duration of the last scrape",
			},
		),

		scrapeErrors: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "walg_scrape_errors_total",
				Help: "Total number of scrape errors",
			},
		),
	}, nil
}

// Describe implements the Prometheus Collector interface
func (e *WalgExporter) Describe(ch chan<- *prometheus.Desc) {
	e.backupLag.Describe(ch)
	e.walLag.Describe(ch)
	e.lsnLag.Describe(ch)
	e.pitrWindow.Describe(ch)
	e.errors.Describe(ch)
	e.walIntegrity.Describe(ch)
	e.backupCount.Describe(ch)
	e.backupTimestamp.Describe(ch)
	e.scrapeDuration.Describe(ch)
	e.scrapeErrors.Describe(ch)
}

// Collect implements the Prometheus Collector interface
func (e *WalgExporter) Collect(ch chan<- prometheus.Metric) {
	e.backupLag.Collect(ch)
	e.walLag.Collect(ch)
	e.lsnLag.Collect(ch)
	e.pitrWindow.Collect(ch)
	e.errors.Collect(ch)
	e.walIntegrity.Collect(ch)
	e.backupCount.Collect(ch)
	e.backupTimestamp.Collect(ch)
	e.scrapeDuration.Collect(ch)
	e.scrapeErrors.Collect(ch)
}

// Start begins the metrics collection loop
func (e *WalgExporter) Start(ctx context.Context) {
	ticker := time.NewTicker(e.scrapeInterval)
	defer ticker.Stop()

	// Initial scrape
	e.scrapeMetrics()

	for {
		select {
		case <-ctx.Done():
			log.Println("Exporter context cancelled, stopping metrics collection")
			return
		case <-ticker.C:
			e.scrapeMetrics()
		}
	}
}

// scrapeMetrics collects all metrics from WAL-G
func (e *WalgExporter) scrapeMetrics() {
	start := time.Now()
	defer func() {
		e.scrapeDuration.Set(time.Since(start).Seconds())
		e.lastScrape = time.Now()
	}()

	log.Printf("Scraping WAL-G metrics...")

	// Get backup information
	backups, err := e.getBackupInfo()
	if err != nil {
		log.Printf("Error getting backup info: %v", err)
		e.scrapeErrors.Inc()
		e.errors.WithLabelValues("backup-list", "command_failed").Inc()
		return
	}

	// Get WAL information
	walInfo, err := e.getWalInfo()
	if err != nil {
		log.Printf("Error getting WAL info: %v", err)
		e.scrapeErrors.Inc()
		e.errors.WithLabelValues("wal-show", "command_failed").Inc()
		return
	}

	// Update backup metrics
	e.updateBackupMetrics(backups)

	// Update WAL metrics
	e.updateWalMetrics(walInfo)

	// Calculate PITR window
	e.updatePitrWindow(backups, walInfo)

	log.Printf("Metrics scrape completed in %v", time.Since(start))
}

// getBackupInfo executes wal-g backup-list --detail --json
func (e *WalgExporter) getBackupInfo() ([]BackupInfo, error) {
	cmd := exec.Command(e.walgPath, "backup-list", "--detail", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute backup-list: %w", err)
	}

	var backups []BackupInfo
	if err := json.Unmarshal(output, &backups); err != nil {
		return nil, fmt.Errorf("failed to parse backup-list output: %w", err)
	}

	return backups, nil
}

// getWalInfo executes wal-g wal-show --detailed-json
func (e *WalgExporter) getWalInfo() (*WalInfo, error) {
	cmd := exec.Command(e.walgPath, "wal-show", "--detailed-json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute wal-show: %w", err)
	}

	var walInfo WalInfo
	if err := json.Unmarshal(output, &walInfo); err != nil {
		return nil, fmt.Errorf("failed to parse wal-show output: %w", err)
	}

	return &walInfo, nil
}

// updateBackupMetrics updates backup-related metrics
func (e *WalgExporter) updateBackupMetrics(backups []BackupInfo) {
	now := time.Now()

	// Reset counters
	e.backupCount.Reset()
	e.backupTimestamp.Reset()
	e.backupLag.Reset()

	var lastFull, lastDelta time.Time
	fullCount, deltaCount := 0, 0

	for _, backup := range backups {
		if backup.IsFull {
			fullCount++
			if backup.Time.After(lastFull) {
				lastFull = backup.Time
			}
		} else {
			deltaCount++
			if backup.Time.After(lastDelta) {
				lastDelta = backup.Time
			}
		}
	}

	// Set backup counts
	e.backupCount.WithLabelValues("full").Set(float64(fullCount))
	e.backupCount.WithLabelValues("delta").Set(float64(deltaCount))

	// Set backup timestamps and lag
	if !lastFull.IsZero() {
		e.backupTimestamp.WithLabelValues("full").Set(float64(lastFull.Unix()))
		e.backupLag.WithLabelValues("full").Set(now.Sub(lastFull).Seconds())
	}

	if !lastDelta.IsZero() {
		e.backupTimestamp.WithLabelValues("delta").Set(float64(lastDelta.Unix()))
		e.backupLag.WithLabelValues("delta").Set(now.Sub(lastDelta).Seconds())
	}
}

// updateWalMetrics updates WAL-related metrics
func (e *WalgExporter) updateWalMetrics(walInfo *WalInfo) {
	// Reset metrics
	e.walIntegrity.Reset()

	// Set WAL integrity status
	for _, detail := range walInfo.Integrity.Details {
		timelineStr := strconv.Itoa(detail.TimelineID)
		var status float64
		if detail.Status == "FOUND" {
			status = 1
		} else {
			status = 0
		}
		e.walIntegrity.WithLabelValues(timelineStr).Set(status)
	}

	// TODO: Implement WAL lag and LSN lag calculations
	// This requires more complex logic to determine the current WAL position
	// and calculate the lag from the latest WAL segments
}

// updatePitrWindow calculates and updates the PITR window size
func (e *WalgExporter) updatePitrWindow(backups []BackupInfo, walInfo *WalInfo) {
	if len(backups) == 0 {
		e.pitrWindow.Set(0)
		return
	}

	// Find the oldest backup
	var oldestBackup time.Time
	for _, backup := range backups {
		if oldestBackup.IsZero() || backup.Time.Before(oldestBackup) {
			oldestBackup = backup.Time
		}
	}

	// PITR window is from the oldest backup to now
	// In a real implementation, this should be to the latest WAL segment
	pitrWindow := time.Since(oldestBackup).Seconds()
	e.pitrWindow.Set(pitrWindow)
}
