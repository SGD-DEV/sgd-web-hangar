package services

import "time"

type Status string

const (
	StatusRunning      Status = "running"
	StatusStopped      Status = "stopped"
	StatusStarting     Status = "starting"
	StatusInitializing Status = "initializing" // first-run only: data dir bootstrap
	StatusStopping     Status = "stopping"
	StatusError        Status = "error"
)

type ServiceStatus struct {
	Name      string `json:"name"`
	Status    Status `json:"status"`
	Port      int    `json:"port"`
	Version   string `json:"version"`
	PID       int    `json:"pid"`
	Uptime    string `json:"uptime"`
	StartedAt string `json:"started_at"`
	Error     string `json:"error,omitempty"`
}

type Service interface {
	Name() string
	Start() error
	Stop() error
	Restart() error
	Status() ServiceStatus
	Logs(lines int) []string
	Version() string
	SetVersion(version string) error
	IsInstalled() bool
}

type BaseService struct {
	ServiceName string
	ServicePort int
	StartTime   time.Time
	ProcessPID  int
	Running     bool
	LastError   string
	StatusText  Status // Tracks intermediate states like "starting", "stopping"
}

func (b *BaseService) GetUptime() string {
	if !b.Running || b.StartTime.IsZero() {
		return ""
	}
	d := time.Since(b.StartTime)
	if d.Hours() >= 24 {
		days := int(d.Hours() / 24)
		hours := int(d.Hours()) % 24
		return formatDuration(days, hours)
	}
	if d.Hours() >= 1 {
		return formatHoursMinutes(int(d.Hours()), int(d.Minutes())%60)
	}
	return formatMinutesSeconds(int(d.Minutes()), int(d.Seconds())%60)
}

func formatDuration(days, hours int) string {
	if days == 1 {
		return "1 day"
	}
	return intToStr(days) + "d " + intToStr(hours) + "h"
}

func formatHoursMinutes(hours, minutes int) string {
	return intToStr(hours) + "h " + intToStr(minutes) + "m"
}

func formatMinutesSeconds(minutes, seconds int) string {
	return intToStr(minutes) + "m " + intToStr(seconds) + "s"
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
