package main

import (
	"reflect"
	"testing"
)

func TestStripUnattendedFlag(t *testing.T) {
	got := stripUnattendedFlag([]string{"-u", "--config", `.\client\configs\agent.yaml`})
	want := []string{"--config", `.\client\configs\agent.yaml`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if stripUnattendedFlag([]string{"--config", "agent.yaml"})[0] != "--config" {
		t.Fatal("left unrelated flags alone")
	}
}
