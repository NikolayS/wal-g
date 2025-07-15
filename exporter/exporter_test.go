package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestParseLSN(t *testing.T) {
	tests := []struct {
		name     string
		lsnStr   string
		expected LSN
		wantErr  bool
	}{
		{
			name:     "valid LSN",
			lsnStr:   "0/1A2B3C4D",
			expected: LSN(0x1A2B3C4D),
			wantErr:  false,
		},
		{
			name:     "valid LSN with high part",
			lsnStr:   "1/0",
			expected: LSN(0x100000000),
			wantErr:  false,
		},
		{
			name:     "empty LSN",
			lsnStr:   "",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid format",
			lsnStr:   "invalid",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid high part",
			lsnStr:   "ZZZZ/0",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseLSN(tt.lsnStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseLSN() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if result != tt.expected {
				t.Errorf("ParseLSN() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLSNString(t *testing.T) {
	tests := []struct {
		name     string
		lsn      LSN
		expected string
	}{
		{
			name:     "zero LSN",
			lsn:      LSN(0),
			expected: "0/0",
		},
		{
			name:     "simple LSN",
			lsn:      LSN(0x1A2B3C4D),
			expected: "0/1A2B3C4D",
		},
		{
			name:     "LSN with high part",
			lsn:      LSN(0x100000000),
			expected: "1/0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.lsn.String()
			if result != tt.expected {
				t.Errorf("LSN.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateLSNLag(t *testing.T) {
	tests := []struct {
		name             string
		currentLSN       LSN
		lastArchivedLSN  LSN
		expectedLag      uint64
	}{
		{
			name:             "positive lag",
			currentLSN:       LSN(1000),
			lastArchivedLSN:  LSN(500),
			expectedLag:      500,
		},
		{
			name:             "no lag",
			currentLSN:       LSN(1000),
			lastArchivedLSN:  LSN(1000),
			expectedLag:      0,
		},
		{
			name:             "negative lag (archived ahead)",
			currentLSN:       LSN(500),
			lastArchivedLSN:  LSN(1000),
			expectedLag:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateLSNLag(tt.currentLSN, tt.lastArchivedLSN)
			if result != tt.expectedLag {
				t.Errorf("calculateLSNLag() = %v, want %v", result, tt.expectedLag)
			}
		})
	}
}

func TestCalculateWalLag(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name        string
		lastWalTime time.Time
		expected    float64
	}{
		{
			name:        "recent WAL",
			lastWalTime: now.Add(-5 * time.Minute),
			expected:    300.0, // 5 minutes in seconds
		},
		{
			name:        "zero time",
			lastWalTime: time.Time{},
			expected:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateWalLag(tt.lastWalTime)
			if tt.expected == 0.0 {
				if result != 0.0 {
					t.Errorf("calculateWalLag() = %v, want %v", result, tt.expected)
				}
			} else {
				// Allow some tolerance for timing differences
				if result < tt.expected-1 || result > tt.expected+1 {
					t.Errorf("calculateWalLag() = %v, want approximately %v", result, tt.expected)
				}
			}
		})
	}
}

func TestNewWalgExporter(t *testing.T) {
	walgPath := "/usr/bin/wal-g"
	scrapeInterval := 30 * time.Second

	exporter, err := NewWalgExporter(walgPath, scrapeInterval)
	if err != nil {
		t.Fatalf("NewWalgExporter() error = %v", err)
	}

	if exporter.walgPath != walgPath {
		t.Errorf("NewWalgExporter() walgPath = %v, want %v", exporter.walgPath, walgPath)
	}

	if exporter.scrapeInterval != scrapeInterval {
		t.Errorf("NewWalgExporter() scrapeInterval = %v, want %v", exporter.scrapeInterval, scrapeInterval)
	}

	// Test that all metrics are initialized
	if exporter.backupLag == nil {
		t.Error("backupLag metric not initialized")
	}
	if exporter.walLag == nil {
		t.Error("walLag metric not initialized")
	}
	if exporter.lsnLag == nil {
		t.Error("lsnLag metric not initialized")
	}
	if exporter.pitrWindow == nil {
		t.Error("pitrWindow metric not initialized")
	}
}

func TestUpdateBackupMetrics(t *testing.T) {
	exporter, err := NewWalgExporter("wal-g", time.Minute)
	if err != nil {
		t.Fatalf("NewWalgExporter() error = %v", err)
	}

	now := time.Now()
	backups := []BackupInfo{
		{
			BackupName: "backup_1",
			Time:       now.Add(-2 * time.Hour),
			IsFull:     true,
		},
		{
			BackupName: "backup_2",
			Time:       now.Add(-1 * time.Hour),
			IsFull:     false,
		},
		{
			BackupName: "backup_3",
			Time:       now.Add(-30 * time.Minute),
			IsFull:     true,
		},
	}

	exporter.updateBackupMetrics(backups)

	// Check backup counts
	fullCount := testutil.ToFloat64(exporter.backupCount.WithLabelValues("full"))
	if fullCount != 2 {
		t.Errorf("Full backup count = %v, want 2", fullCount)
	}

	deltaCount := testutil.ToFloat64(exporter.backupCount.WithLabelValues("delta"))
	if deltaCount != 1 {
		t.Errorf("Delta backup count = %v, want 1", deltaCount)
	}

	// Check that lag metrics are set (should be positive)
	fullLag := testutil.ToFloat64(exporter.backupLag.WithLabelValues("full"))
	if fullLag <= 0 {
		t.Errorf("Full backup lag = %v, want > 0", fullLag)
	}

	deltaLag := testutil.ToFloat64(exporter.backupLag.WithLabelValues("delta"))
	if deltaLag <= 0 {
		t.Errorf("Delta backup lag = %v, want > 0", deltaLag)
	}
}

func TestUpdateWalMetrics(t *testing.T) {
	exporter, err := NewWalgExporter("wal-g", time.Minute)
	if err != nil {
		t.Fatalf("NewWalgExporter() error = %v", err)
	}

	walInfo := &WalInfo{
		Integrity: struct {
			Status  string `json:"status"`
			Details []struct {
				TimelineID    int    `json:"timeline_id"`
				StartSegment  string `json:"start_segment"`
				EndSegment    string `json:"end_segment"`
				SegmentsCount int    `json:"segments_count"`
				Status        string `json:"status"`
			} `json:"details"`
		}{
			Status: "OK",
			Details: []struct {
				TimelineID    int    `json:"timeline_id"`
				StartSegment  string `json:"start_segment"`
				EndSegment    string `json:"end_segment"`
				SegmentsCount int    `json:"segments_count"`
				Status        string `json:"status"`
			}{
				{
					TimelineID:    1,
					StartSegment:  "000000010000000000000001",
					EndSegment:    "000000010000000000000010",
					SegmentsCount: 10,
					Status:        "FOUND",
				},
				{
					TimelineID:    2,
					StartSegment:  "000000020000000000000001",
					EndSegment:    "000000020000000000000005",
					SegmentsCount: 5,
					Status:        "FOUND",
				},
			},
		},
	}

	exporter.updateWalMetrics(walInfo)

	// Check WAL integrity status
	timeline1Status := testutil.ToFloat64(exporter.walIntegrity.WithLabelValues("1"))
	if timeline1Status != 1 {
		t.Errorf("Timeline 1 integrity status = %v, want 1", timeline1Status)
	}

	timeline2Status := testutil.ToFloat64(exporter.walIntegrity.WithLabelValues("2"))
	if timeline2Status != 1 {
		t.Errorf("Timeline 2 integrity status = %v, want 1", timeline2Status)
	}
}

func TestUpdatePitrWindow(t *testing.T) {
	exporter, err := NewWalgExporter("wal-g", time.Minute)
	if err != nil {
		t.Fatalf("NewWalgExporter() error = %v", err)
	}

	now := time.Now()
	backups := []BackupInfo{
		{
			BackupName: "backup_1",
			Time:       now.Add(-24 * time.Hour),
			IsFull:     true,
		},
		{
			BackupName: "backup_2",
			Time:       now.Add(-12 * time.Hour),
			IsFull:     false,
		},
	}

	walInfo := &WalInfo{} // Empty WAL info for this test

	exporter.updatePitrWindow(backups, walInfo)

	pitrWindow := testutil.ToFloat64(exporter.pitrWindow)
	// Should be approximately 24 hours (86400 seconds)
	if pitrWindow < 86300 || pitrWindow > 86500 {
		t.Errorf("PITR window = %v, want approximately 86400", pitrWindow)
	}
}

func TestCalculatePitrWindow(t *testing.T) {
	now := time.Now()
	backups := []BackupInfo{
		{
			BackupName: "backup_1",
			Time:       now.Add(-2 * time.Hour),
			IsFull:     true,
		},
		{
			BackupName: "backup_2",
			Time:       now.Add(-1 * time.Hour),
			IsFull:     false,
		},
	}

	walInfo := &WalInfo{}
	window := calculatePitrWindow(backups, walInfo)

	if !window.IsValid {
		t.Error("PITR window should be valid")
	}

	expectedOldest := now.Add(-2 * time.Hour)
	if !window.OldestBackupTime.Equal(expectedOldest) {
		t.Errorf("OldestBackupTime = %v, want %v", window.OldestBackupTime, expectedOldest)
	}

	// Window should be approximately 2 hours
	if window.WindowSeconds < 7100 || window.WindowSeconds > 7300 {
		t.Errorf("WindowSeconds = %v, want approximately 7200", window.WindowSeconds)
	}
}

func TestValidatePitrWindow(t *testing.T) {
	tests := []struct {
		name     string
		window   *PitrWindow
		expected bool
	}{
		{
			name: "valid window",
			window: &PitrWindow{
				IsValid:       true,
				WindowSeconds: 3600, // 1 hour
			},
			expected: true,
		},
		{
			name: "invalid window",
			window: &PitrWindow{
				IsValid: false,
			},
			expected: false,
		},
		{
			name: "negative window",
			window: &PitrWindow{
				IsValid:       true,
				WindowSeconds: -100,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validatePitrWindow(tt.window)
			if result != tt.expected {
				t.Errorf("validatePitrWindow() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseWalSegmentName(t *testing.T) {
	tests := []struct {
		name        string
		segmentName string
		wantErr     bool
	}{
		{
			name:        "valid segment name",
			segmentName: "000000010000000000000001",
			wantErr:     false,
		},
		{
			name:        "invalid segment name - too short",
			segmentName: "00000001",
			wantErr:     true,
		},
		{
			name:        "invalid segment name - invalid timeline",
			segmentName: "ZZZZZZZZ0000000000000001",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseWalSegmentName(tt.segmentName)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseWalSegmentName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Mock exporter for testing
type MockExporter struct {
	*WalgExporter
}

func NewMockExporter() *MockExporter {
	exporter, _ := NewWalgExporter("mock-wal-g", time.Minute)
	return &MockExporter{exporter}
}

func TestMockExporter(t *testing.T) {
	mock := NewMockExporter()
	
	// Test that the mock exporter can be created
	if mock.WalgExporter == nil {
		t.Error("Mock exporter should not be nil")
	}
	
	// Test that metrics are properly initialized
	registry := prometheus.NewRegistry()
	registry.MustRegister(mock.WalgExporter)
	
	// This should not panic
	metrics, err := registry.Gather()
	if err != nil {
		t.Errorf("Failed to gather metrics: %v", err)
	}
	
	// Should have our metrics registered
	if len(metrics) == 0 {
		t.Error("No metrics registered")
	}
}

func TestExporterDescribe(t *testing.T) {
	exporter, err := NewWalgExporter("wal-g", time.Minute)
	if err != nil {
		t.Fatalf("NewWalgExporter() error = %v", err)
	}

	ch := make(chan *prometheus.Desc, 100)
	go func() {
		defer close(ch)
		exporter.Describe(ch)
	}()

	var descs []*prometheus.Desc
	for desc := range ch {
		descs = append(descs, desc)
	}

	// Should have all our metrics described
	expectedMetrics := []string{
		"walg_backup_lag_seconds",
		"walg_wal_lag_seconds",
		"walg_lsn_lag_bytes",
		"walg_pitr_window_seconds",
		"walg_errors_total",
		"walg_wal_integrity_status",
		"walg_backup_count",
		"walg_backup_timestamp",
		"walg_scrape_duration_seconds",
		"walg_scrape_errors_total",
	}

	if len(descs) != len(expectedMetrics) {
		t.Errorf("Expected %d metrics, got %d", len(expectedMetrics), len(descs))
	}
}

func TestExporterCollect(t *testing.T) {
	exporter, err := NewWalgExporter("wal-g", time.Minute)
	if err != nil {
		t.Fatalf("NewWalgExporter() error = %v", err)
	}

	ch := make(chan prometheus.Metric, 100)
	go func() {
		defer close(ch)
		exporter.Collect(ch)
	}()

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	// Should have collected some metrics
	if len(metrics) == 0 {
		t.Error("No metrics collected")
	}
}

// Benchmark tests
func BenchmarkParseLSN(b *testing.B) {
	lsnStr := "1A2B3C4D/5E6F7890"
	for i := 0; i < b.N; i++ {
		_, _ = ParseLSN(lsnStr)
	}
}

func BenchmarkCalculateLSNLag(b *testing.B) {
	currentLSN := LSN(1000000)
	lastArchivedLSN := LSN(500000)
	for i := 0; i < b.N; i++ {
		_ = calculateLSNLag(currentLSN, lastArchivedLSN)
	}
}

func BenchmarkUpdateBackupMetrics(b *testing.B) {
	exporter, _ := NewWalgExporter("wal-g", time.Minute)
	now := time.Now()
	backups := []BackupInfo{
		{BackupName: "backup_1", Time: now.Add(-2 * time.Hour), IsFull: true},
		{BackupName: "backup_2", Time: now.Add(-1 * time.Hour), IsFull: false},
		{BackupName: "backup_3", Time: now.Add(-30 * time.Minute), IsFull: true},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exporter.updateBackupMetrics(backups)
	}
} 