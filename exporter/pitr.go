package main

import (
	"fmt"
	"log"
	"time"
)

// PitrWindow represents the point-in-time recovery window information
type PitrWindow struct {
	OldestBackupTime time.Time
	NewestWalTime    time.Time
	WindowSeconds    float64
	IsValid          bool
}

// calculatePitrWindow calculates the PITR window based on backup and WAL information
func calculatePitrWindow(backups []BackupInfo, walInfo *WalInfo) *PitrWindow {
	window := &PitrWindow{
		IsValid: false,
	}

	if len(backups) == 0 {
		log.Printf("No backups found, PITR window is 0")
		return window
	}

	// Find the oldest backup (this is the start of our PITR window)
	var oldestBackup time.Time
	for _, backup := range backups {
		if oldestBackup.IsZero() || backup.Time.Before(oldestBackup) {
			oldestBackup = backup.Time
		}
	}

	window.OldestBackupTime = oldestBackup

	// Find the newest WAL segment time (this is the end of our PITR window)
	// In a real implementation, you'd need to get the timestamp of the latest WAL segment
	// For now, we'll use the current time as an approximation
	newestWalTime := time.Now()

	// In a more sophisticated implementation, you would:
	// 1. Parse the latest WAL segment name from walInfo
	// 2. Get the modification time of that segment from storage
	// 3. Use that as the end of the PITR window

	window.NewestWalTime = newestWalTime
	window.WindowSeconds = newestWalTime.Sub(oldestBackup).Seconds()
	window.IsValid = true

	return window
}

// calculatePitrWindowAdvanced calculates PITR window with more sophisticated logic
func calculatePitrWindowAdvanced(backups []BackupInfo, walInfo *WalInfo) *PitrWindow {
	window := &PitrWindow{
		IsValid: false,
	}

	if len(backups) == 0 {
		return window
	}

	// Find the oldest backup that's still valid for PITR
	var oldestValidBackup time.Time
	for _, backup := range backups {
		// In a real implementation, you might want to exclude certain backups
		// based on their status, corruption, or other factors
		if oldestValidBackup.IsZero() || backup.Time.Before(oldestValidBackup) {
			oldestValidBackup = backup.Time
		}
	}

	window.OldestBackupTime = oldestValidBackup

	// Determine the latest available WAL segment time
	latestWalTime := findLatestWalTime(walInfo)
	if latestWalTime.IsZero() {
		// If we can't determine the latest WAL time, use current time
		latestWalTime = time.Now()
	}

	window.NewestWalTime = latestWalTime
	window.WindowSeconds = latestWalTime.Sub(oldestValidBackup).Seconds()
	window.IsValid = true

	return window
}

// findLatestWalTime attempts to find the timestamp of the latest WAL segment
func findLatestWalTime(walInfo *WalInfo) time.Time {
	// This is a placeholder implementation
	// In a real implementation, you would:
	// 1. Parse the latest WAL segment name from each timeline
	// 2. Query the storage system for the modification time of that segment
	// 3. Return the latest timestamp found

	// For now, we'll return zero time to indicate we couldn't determine it
	return time.Time{}
}

// validatePitrWindow checks if the PITR window is reasonable
func validatePitrWindow(window *PitrWindow) bool {
	if !window.IsValid {
		return false
	}

	// Check if the window is not negative
	if window.WindowSeconds < 0 {
		log.Printf("Invalid PITR window: negative duration")
		return false
	}

	// Check if the window is not too large (e.g., more than 1 year)
	maxWindowSeconds := float64(365 * 24 * 60 * 60) // 1 year in seconds
	if window.WindowSeconds > maxWindowSeconds {
		log.Printf("Warning: PITR window is very large: %.2f days", window.WindowSeconds/86400)
	}

	return true
}

// Enhanced updatePitrWindow with better calculation
func (e *WalgExporter) updatePitrWindowAdvanced(backups []BackupInfo, walInfo *WalInfo) {
	window := calculatePitrWindowAdvanced(backups, walInfo)

	if validatePitrWindow(window) {
		e.pitrWindow.Set(window.WindowSeconds)
		log.Printf("PITR window: %.2f hours (from %v to %v)",
			window.WindowSeconds/3600,
			window.OldestBackupTime.Format("2006-01-02 15:04:05"),
			window.NewestWalTime.Format("2006-01-02 15:04:05"))
	} else {
		e.pitrWindow.Set(0)
		log.Printf("Invalid PITR window, setting to 0")
	}
}

// getPitrWindowStatus returns a human-readable status of the PITR window
func getPitrWindowStatus(window *PitrWindow) string {
	if !window.IsValid {
		return "Invalid"
	}

	hours := window.WindowSeconds / 3600
	if hours < 1 {
		return "Less than 1 hour"
	} else if hours < 24 {
		return fmt.Sprintf("%.1f hours", hours)
	} else {
		days := hours / 24
		return fmt.Sprintf("%.1f days", days)
	}
}
