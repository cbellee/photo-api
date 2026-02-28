package main

import (
	"testing"
)

func TestParseTagQuery_ContainerOnly(t *testing.T) {
	conds, err := ParseTagQuery("@container='images'")
	if err != nil {
		t.Fatal(err)
	}
	if len(conds) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(conds))
	}
	if !conds[0].IsContainer || conds[0].Value != "images" {
		t.Fatalf("unexpected condition: %+v", conds[0])
	}
}

func TestParseTagQuery_MultipleAND(t *testing.T) {
	q := "@container='images' AND collection='trips' AND album='hong kong' AND isDeleted='false'"
	conds, err := ParseTagQuery(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(conds) != 4 {
		t.Fatalf("expected 4 conditions, got %d", len(conds))
	}

	// First is @container
	if !conds[0].IsContainer || conds[0].Value != "images" {
		t.Errorf("cond[0]: %+v", conds[0])
	}
}