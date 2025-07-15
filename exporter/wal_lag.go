package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// LSN represents a PostgreSQL Log Sequence Number
type LSN uint64

// ParseLSN parses a PostgreSQL LSN string (e.g., "0/1A2B3C4D") into an LSN value
func ParseLSN(lsnStr string) (LSN, error) {
	if lsnStr == "" {
		return 0, fmt.Errorf("empty LSN string")
	}

	parts := strings.Split(lsnStr, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid LSN format: %s", lsnStr)
	}

	// Parse the high 32 bits
	high, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN high part: %s", parts[0])
	}

	// Parse the low 32 bits
	low, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN low part: %s", parts[1])
	}

	// Combine high and low parts
	lsn := LSN((high << 32) | low)
	return lsn, nil
}

// String returns the LSN as a string in PostgreSQL format
func (lsn LSN) String() string {
	high := uint32(lsn >> 32)
	low := uint32(lsn)
	return fmt.Sprintf("%X/%X", high, low)
}

// Bytes returns the LSN as bytes
func (lsn LSN) Bytes() uint64 {
	return uint64(lsn)
}

// WalSegmentInfo represents information about a WAL segment
type WalSegmentInfo struct {
	Name      string
	Timeline  int
	LSN       LSN
	Timestamp time.Time
}

// calculateLSNLag calculates the LSN lag in bytes between two LSNs
func calculateLSNLag(currentLSN, lastArchivedLSN LSN) uint64 {
	if currentLSN > lastArchivedLSN {
		return uint64(currentLSN - lastArchivedLSN)
	}
	return 0
}

// calculateWalLag calculates the time lag since the last WAL segment
func calculateWalLag(lastWalTime time.Time) float64 {
	if lastWalTime.IsZero() {
		return 0
	}
	return time.Since(lastWalTime).Seconds()
}

// parseWalSegmentName extracts timeline and LSN information from a WAL segment name
func parseWalSegmentName(segmentName string) (*WalSegmentInfo, error) {
	if len(segmentName) < 24 {
		return nil, fmt.Errorf("invalid WAL segment name: %s", segmentName)
	}

	// Extract timeline (first 8 characters)
	timelineStr := segmentName[0:8]
	timeline, err := strconv.ParseInt(timelineStr, 16, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid timeline in segment name: %s", timelineStr)
	}

	// For now, we'll approximate the LSN from the segment name
	// In a real implementation, you'd need to calculate the actual LSN
	// based on the segment number and WAL segment size
	segmentInfo := &WalSegmentInfo{
		Name:     segmentName,
		Timeline: int(timeline),
		LSN:      0, // TODO: Calculate actual LSN from segment name
	}

	return segmentInfo, nil
}

// getCurrentLSN would get the current LSN from PostgreSQL
// This is a placeholder - in a real implementation, you'd connect to PostgreSQL
// and run "SELECT pg_current_wal_lsn();" or similar
func getCurrentLSN() (LSN, error) {
	// Placeholder implementation
	// In a real implementation, you would:
	// 1. Connect to PostgreSQL
	// 2. Execute: SELECT pg_current_wal_lsn();
	// 3. Parse the result
	return 0, fmt.Errorf("not implemented - would require PostgreSQL connection")
}

// getLastArchivedLSN would get the last archived LSN
// This could be determined from the latest WAL segment in storage
func getLastArchivedLSN(walInfo *WalInfo) (LSN, error) {
	// Find the latest WAL segment across all timelines
	var latestLSN LSN
	
	for _, detail := range walInfo.Integrity.Details {
		if detail.EndSegment != "" {
			// Parse the end segment to get approximate LSN
			segmentInfo, err := parseWalSegmentName(detail.EndSegment)
			if err != nil {
				continue
			}
			
			// This is a simplified approach - in reality, you'd need to
			// calculate the actual LSN from the segment name
			if segmentInfo.LSN > latestLSN {
				latestLSN = segmentInfo.LSN
			}
		}
	}
	
	return latestLSN, nil
}

// updateWalLagMetrics updates WAL and LSN lag metrics
func (e *WalgExporter) updateWalLagMetrics(walInfo *WalInfo) {
	// Reset metrics
	e.walLag.Reset()
	e.lsnLag.Reset()
	
	// For each timeline, calculate lag
	for _, detail := range walInfo.Integrity.Details {
		timelineStr := strconv.Itoa(detail.TimelineID)
		
		// Calculate time-based lag (placeholder)
		// In a real implementation, you'd need to determine when the last
		// WAL segment was archived for this timeline
		walLagSeconds := float64(0) // TODO: Implement actual time lag calculation
		e.walLag.WithLabelValues(timelineStr).Set(walLagSeconds)
		
		// Calculate LSN-based lag (placeholder)
		// In a real implementation, you'd compare current LSN with last archived LSN
		lsnLagBytes := float64(0) // TODO: Implement actual LSN lag calculation
		e.lsnLag.WithLabelValues(timelineStr).Set(lsnLagBytes)
	}
}

// Enhanced updateWalMetrics with lag calculations
func (e *WalgExporter) updateWalMetricsWithLag(walInfo *WalInfo) {
	// Update integrity status
	e.updateWalMetrics(walInfo)
	
	// Update lag metrics
	e.updateWalLagMetrics(walInfo)
} 