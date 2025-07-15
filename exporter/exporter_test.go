package main

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestParseLSN(t *testing.T) {
	tests := []struct {
		name        string
		lsnStr      string
		expected    LSN
		expectError bool
	}{
		{
			name:        "valid LSN",
			lsnStr:      "0/1A2B3C4D",
			expected:    LSN(0x1A2B3C4D),
			expectError: false,
		},
		{
			name:        "another valid LSN",
			lsnStr:      "1/ABCDEF12",
			expected:    LSN((1 << 32) | 0xABCDEF12),
			expectError: false,
		},
		{
			name:        "empty string",
			lsnStr:      "",
			expected:    0,
			expectError: true,
		},
		{
			name:        "invalid format",
			lsnStr:      "invalid",
			expected:    0,
			expectError: true,
		},
		{
			name:        "invalid hex",
			lsnStr:      "0/ZZZZZZZZ",
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseLSN(tt.lsnStr)
			if tt.expectError {
				if err == nil {
					t.Errorf("ParseLSN() expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("ParseLSN() unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("ParseLSN() = %v, want %v", result, tt.expected)
				}
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
			name:     "LSN with timeline",
			lsn:      LSN((1 << 32) | 0xABCDEF12),
			expected: "1/ABCDEF12",
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
		name            string
		currentLSN      LSN
		lastArchivedLSN LSN
		expectedLag     uint64
	}{
		{
			name:            "positive lag",
			currentLSN:      LSN(1000),
			lastArchivedLSN: LSN(500),
			expectedLag:     500,
		},
		{
			name:            "no lag",
			currentLSN:      LSN(1000),
			lastArchivedLSN: LSN(1000),
			expectedLag:     0,
		},
		{
			name:            "negative lag (archived ahead)",
			currentLSN:      LSN(500),
			lastArchivedLSN: LSN(1000),
			expectedLag:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateLSNLag(tt.currentLSN, tt.lastArchivedLSN)
			if result != tt.expectedLag {
				t.Errorf("CalculateLSNLag() = %v, want %v", result, tt.expectedLag)
			}
		})
	}
}

func TestCalculatePitrWindow(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-time.Hour)

	tests := []struct {
		name     string
		backups  []BackupInfo
		walInfo  *WalInfo
		expected bool // whether window should be valid
	}{
		{
			name:     "no backups",
			backups:  []BackupInfo{},
			walInfo:  &WalInfo{},
			expected: false,
		},
		{
			name: "valid backups",
			backups: []BackupInfo{
				{
					BackupName: "backup1",
					Time:       oneHourAgo,
					IsFull:     true,
				},
			},
			walInfo:  &WalInfo{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePitrWindow(tt.backups, tt.walInfo)
			if result.IsValid != tt.expected {
				t.Errorf("calculatePitrWindow() validity = %v, want %v", result.IsValid, tt.expected)
			}
		})
	}
}

func TestWalgExporter(t *testing.T) {
	// Create a new exporter
	exporter, err := NewWalgExporter("echo", time.Minute)
	if err != nil {
		t.Fatalf("NewWalgExporter() error = %v", err)
	}

	// Test that metrics are registered
	registry := prometheus.NewRegistry()
	registry.MustRegister(exporter)

	// Test that we can collect metrics
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Errorf("Failed to gather metrics: %v", err)
	}

	if len(metricFamilies) == 0 {
		t.Error("No metrics collected")
	}
}

func TestMockWalgExporter(t *testing.T) {
	// Create a mock exporter for testing
	mockExporter := &MockWalgExporter{}

	// Test backup lag
	mockExporter.SetBackupLag("full", 3600)
	lag := mockExporter.GetBackupLag("full")
	if lag != 3600 {
		t.Errorf("Expected backup lag 3600, got %f", lag)
	}

	// Test WAL lag
	mockExporter.SetWalLag("1", 60)
	walLag := mockExporter.GetWalLag("1")
	if walLag != 60 {
		t.Errorf("Expected WAL lag 60, got %f", walLag)
	}

	// Test LSN lag
	mockExporter.SetLsnLag("1", 1024)
	lsnLag := mockExporter.GetLsnLag("1")
	if lsnLag != 1024 {
		t.Errorf("Expected LSN lag 1024, got %f", lsnLag)
	}

	// Test PITR window
	mockExporter.SetPitrWindow(86400)
	pitrWindow := mockExporter.GetPitrWindow()
	if pitrWindow != 86400 {
		t.Errorf("Expected PITR window 86400, got %f", pitrWindow)
	}
}

// MockWalgExporter is a mock implementation for testing
type MockWalgExporter struct {
	backupLag  map[string]float64
	walLag     map[string]float64
	lsnLag     map[string]float64
	pitrWindow float64
}

func (m *MockWalgExporter) SetBackupLag(backupType string, seconds float64) {
	if m.backupLag == nil {
		m.backupLag = make(map[string]float64)
	}
	m.backupLag[backupType] = seconds
}

func (m *MockWalgExporter) GetBackupLag(backupType string) float64 {
	if m.backupLag == nil {
		return 0
	}
	return m.backupLag[backupType]
}

func (m *MockWalgExporter) SetWalLag(timeline string, seconds float64) {
	if m.walLag == nil {
		m.walLag = make(map[string]float64)
	}
	m.walLag[timeline] = seconds
}

func (m *MockWalgExporter) GetWalLag(timeline string) float64 {
	if m.walLag == nil {
		return 0
	}
	return m.walLag[timeline]
}

func (m *MockWalgExporter) SetLsnLag(timeline string, bytes float64) {
	if m.lsnLag == nil {
		m.lsnLag = make(map[string]float64)
	}
	m.lsnLag[timeline] = bytes
}

func (m *MockWalgExporter) GetLsnLag(timeline string) float64 {
	if m.lsnLag == nil {
		return 0
	}
	return m.lsnLag[timeline]
}

func (m *MockWalgExporter) SetPitrWindow(seconds float64) {
	m.pitrWindow = seconds
}

func (m *MockWalgExporter) GetPitrWindow() float64 {
	return m.pitrWindow
}

// Benchmark tests
func BenchmarkParseLSN(b *testing.B) {
	lsnStr := "1/ABCDEF12"
	for i := 0; i < b.N; i++ {
		_, _ = ParseLSN(lsnStr)
	}
}

func BenchmarkCalculateLSNLag(b *testing.B) {
	currentLSN := LSN(1000000)
	lastArchivedLSN := LSN(500000)
	for i := 0; i < b.N; i++ {
		_ = CalculateLSNLag(currentLSN, lastArchivedLSN)
	}
}

func BenchmarkCalculatePitrWindow(b *testing.B) {
	backups := []BackupInfo{
		{
			BackupName: "backup1",
			Time:       time.Now().Add(-time.Hour),
			IsFull:     true,
		},
	}
	walInfo := &WalInfo{}

	for i := 0; i < b.N; i++ {
		_ = calculatePitrWindow(backups, walInfo)
	}
}
