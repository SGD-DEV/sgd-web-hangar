package downloader

import "fmt"

func FormatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func FormatSpeed(mbps float64) string {
	if mbps >= 1.0 {
		return fmt.Sprintf("%.1f MB/s", mbps)
	}
	return fmt.Sprintf("%.0f KB/s", mbps*1024)
}

func FormatETA(seconds int) string {
	if seconds <= 0 {
		return "calculating..."
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%dm %ds", minutes, secs)
}
