package state

import (
	"fmt"
	"testing"
)

func TestAgentBeforeCreateFillsJSON(t *testing.T) {
	agent := Agent{ID: "a"}
	if err := agent.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if agent.LabelsJSON != "{}" || agent.ProcessesJSON != "[]" || agent.TemperaturesJSON != "[]" {
		t.Fatalf("got labels=%q processes=%q temps=%q", agent.LabelsJSON, agent.ProcessesJSON, agent.TemperaturesJSON)
	}
	agent.LabelsJSON = `{"role":"web"}`
	agent.ProcessesJSON = `[{"pid":1}]`
	if err := agent.BeforeCreate(nil); err != nil {
		t.Fatal(err)
	}
	if agent.LabelsJSON != `{"role":"web"}` || agent.ProcessesJSON != `[{"pid":1}]` {
		t.Fatal("overwrote existing json")
	}
}

func TestIsUniqueViolation(t *testing.T) {
	if isUniqueViolation(nil) {
		t.Fatal("nil")
	}
	err := fmt.Errorf(`ERROR: duplicate key value violates unique constraint "metric_receipts_pkey" (SQLSTATE 23505)`)
	if !isUniqueViolation(err) {
		t.Fatal(err)
	}
}
