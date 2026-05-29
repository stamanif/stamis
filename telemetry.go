package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// TelemetryEvent represents a single observable action within the runtime.
type TelemetryEvent struct {
	Timestamp time.Time   `json:"timestamp"`
	Type      string      `json:"type"`
	Data      interface{} `json:"data"`
}

// ClawSniff acts as the non-blocking observability middleware.
type ClawSniff struct {
	eventChan chan TelemetryEvent
	logFile   *os.File
}

var Sniffer *ClawSniff

// InitTelemetry spins up the ClawSniff background worker.
func InitTelemetry() {
	file, err := os.OpenFile("telemetry.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("[Warning] ClawSniff failed to open telemetry.log: %v\n", err)
		return
	}

	Sniffer = &ClawSniff{
		eventChan: make(chan TelemetryEvent, 100),
		logFile:   file,
	}

	go func() {
		for event := range Sniffer.eventChan {
			b, err := json.Marshal(event)
			if err == nil {
				Sniffer.logFile.Write(append(b, '\n'))
			}
		}
	}()
}

// EmitTelemetry fires an event to the background worker without blocking the main thread.
func EmitTelemetry(eventType string, data interface{}) {
	if Sniffer != nil {
		select {
		case Sniffer.eventChan <- TelemetryEvent{
			Timestamp: time.Now(),
			Type:      eventType,
			Data:      data,
		}:
		default:
			// Buffer full, drop the event silently to ensure zero execution latency
		}
	}
}
