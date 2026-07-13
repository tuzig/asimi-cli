package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/afittestide/asimi/storage"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupIncidentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&storage.Incident{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func TestCreateIncidentTool(t *testing.T) {
	db := setupIncidentTestDB(t)
	ctx := context.Background()

	tool := CreateIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}

	result, err := tool.Call(ctx, `{"description":"segfault in main","severity":"critical"}`)
	if err != nil {
		t.Fatalf("CreateIncident failed: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	// Verify the incident was created with correct fields
	var incident storage.Incident
	if err := db.Where("username = ? AND project = ?", "testuser", "testproject").First(&incident).Error; err != nil {
		t.Fatalf("failed to query incident: %v", err)
	}

	if incident.Description != "segfault in main" {
		t.Errorf("Description = %q, want %q", incident.Description, "segfault in main")
	}
	if incident.Severity != "critical" {
		t.Errorf("Severity = %q, want %q", incident.Severity, "critical")
	}
	if incident.Status != "open" {
		t.Errorf("Status = %q, want %q", incident.Status, "open")
	}
	if incident.CommitHash != "" {
		t.Errorf("CommitHash should be empty, got %q", incident.CommitHash)
	}
}

func TestCreateIncidentTool_NoCollision(t *testing.T) {
	db := setupIncidentTestDB(t)
	ctx := context.Background()

	tool := CreateIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}

	// Create two incidents with identical description and severity
	_, err := tool.Call(ctx, `{"description":"segfault in main","severity":"critical"}`)
	if err != nil {
		t.Fatalf("first CreateIncident failed: %v", err)
	}
	_, err = tool.Call(ctx, `{"description":"segfault in main","severity":"critical"}`)
	if err != nil {
		t.Fatalf("second CreateIncident failed: %v", err)
	}

	// Both should exist as separate rows
	var count int64
	db.Model(&storage.Incident{}).Where("username = ? AND project = ?", "testuser", "testproject").Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 incidents, got %d", count)
	}
}

func TestCreateIncidentTool_MissingFields(t *testing.T) {
	db := setupIncidentTestDB(t)
	ctx := context.Background()

	tool := CreateIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}

	// Missing severity
	_, err := tool.Call(ctx, `{"description":"something broke"}`)
	if err == nil {
		t.Error("expected error when severity is missing")
	}

	// Missing description
	_, err = tool.Call(ctx, `{"severity":"high"}`)
	if err == nil {
		t.Error("expected error when description is missing")
	}
}

func TestResolveIncidentTool(t *testing.T) {
	db := setupIncidentTestDB(t)
	ctx := context.Background()

	// Create an incident first
	createTool := CreateIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}
	_, err := createTool.Call(ctx, `{"description":"db down","severity":"high"}`)
	if err != nil {
		t.Fatalf("CreateIncident failed: %v", err)
	}

	var incident storage.Incident
	if err := db.First(&incident).Error; err != nil {
		t.Fatalf("failed to query incident: %v", err)
	}

	// Now resolve it
	resolveTool := ResolveIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}
	_, err = resolveTool.Call(ctx, `{"incident_id":"`+incident.IncidentID+`","resolution":"restarted db","root_cause":"connection pool exhaustion"}`)
	if err != nil {
		t.Fatalf("ResolveIncident failed: %v", err)
	}

	// Verify resolution
	var resolved storage.Incident
	if err := db.First(&resolved).Error; err != nil {
		t.Fatalf("failed to query resolved incident: %v", err)
	}
	if resolved.Status != "resolved" {
		t.Errorf("Status = %q, want %q", resolved.Status, "resolved")
	}
	if resolved.Resolution != "restarted db" {
		t.Errorf("Resolution = %q, want %q", resolved.Resolution, "restarted db")
	}
	if resolved.RootCause != "connection pool exhaustion" {
		t.Errorf("RootCause = %q, want %q", resolved.RootCause, "connection pool exhaustion")
	}
}

func TestResolveIncidentTool_NotFound(t *testing.T) {
	db := setupIncidentTestDB(t)
	ctx := context.Background()

	resolveTool := ResolveIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}
	_, err := resolveTool.Call(ctx, `{"incident_id":"nonexistent","resolution":"wont work"}`)
	if err == nil {
		t.Error("expected error for nonexistent incident")
	}
}

func TestGetIncidentTool(t *testing.T) {
	db := setupIncidentTestDB(t)
	ctx := context.Background()

	// Create an incident first
	createTool := CreateIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}
	_, err := createTool.Call(ctx, `{"description":"memory leak","severity":"medium"}`)
	if err != nil {
		t.Fatalf("CreateIncident failed: %v", err)
	}

	var incident storage.Incident
	if err := db.First(&incident).Error; err != nil {
		t.Fatalf("failed to query incident: %v", err)
	}

	// Now get it
	getTool := GetIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}
	result, err := getTool.Call(ctx, `{"incident_id":"`+incident.IncidentID+`"}`)
	if err != nil {
		t.Fatalf("GetIncident failed: %v", err)
	}

	var fetched storage.Incident
	if err := json.Unmarshal([]byte(result), &fetched); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if fetched.Description != "memory leak" {
		t.Errorf("Description = %q, want %q", fetched.Description, "memory leak")
	}
	if fetched.Severity != "medium" {
		t.Errorf("Severity = %q, want %q", fetched.Severity, "medium")
	}
	if fetched.Status != "open" {
		t.Errorf("Status = %q, want %q", fetched.Status, "open")
	}
}

func TestGetIncidentTool_NotFound(t *testing.T) {
	db := setupIncidentTestDB(t)
	ctx := context.Background()

	getTool := GetIncidentTool{Ctx: ToolContext{
		DB:       db,
		Username: "testuser",
		Project:  "testproject",
	}}
	_, err := getTool.Call(ctx, `{"incident_id":"nonexistent"}`)
	if err == nil {
		t.Error("expected error for nonexistent incident")
	}
}
