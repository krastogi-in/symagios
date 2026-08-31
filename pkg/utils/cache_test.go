package utils_test

import (
	"testing"

	"symagios/utils"
)

func TestLRUCache(t *testing.T) {
	c := utils.NewLRUCache[string](2)
	c.Add("a", "1")
	c.Add("b", "2")
	if v, ok := c.Get("a"); !ok || v != "1" {
		t.Fatalf("a=%v %v", v, ok)
	}
	c.Remove("a")
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be gone")
	}
}

func TestCacheLatest(t *testing.T) {
	c := utils.NewCache(10)
	c.Add("k1", "v1")
	c.Add("k2", "v2")
	if got, ok := c.Get("k2"); !ok || got != "v2" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if latest := c.GetLatestSummary(); latest != "v2" {
		t.Fatalf("latest=%q", latest)
	}
}
