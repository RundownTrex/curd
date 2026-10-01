package internal

import (
	"testing"
)

func TestGetOrderedCategoriesRofiAndTerminal(t *testing.T) {
	// 1. RofiSelection = true: DOWNLOAD must be absent
	rofiConfig := &CurdConfig{
		RofiSelection: true,
		MenuOrder:     "CURRENT,ALL,UNTRACKED,DOWNLOAD,UPDATE,CONTINUE_LAST,PROVIDER",
	}
	rofiCategories := getOrderedCategories(rofiConfig)
	for _, cat := range rofiCategories {
		if cat.Key == "DOWNLOAD" {
			t.Errorf("expected DOWNLOAD to be excluded from Rofi menu, but found it")
		}
	}

	// 2. RofiSelection = false: DOWNLOAD must be present
	termConfig := &CurdConfig{
		RofiSelection: false,
		MenuOrder:     "CURRENT,ALL,UNTRACKED,DOWNLOAD,UPDATE,CONTINUE_LAST,PROVIDER",
	}
	termCategories := getOrderedCategories(termConfig)
	foundDownload := false
	for _, cat := range termCategories {
		if cat.Key == "DOWNLOAD" {
			foundDownload = true
			break
		}
	}
	if !foundDownload {
		t.Errorf("expected DOWNLOAD to be present in terminal menu, but was missing")
	}

	// 3. Old config where user has MenuOrder without DOWNLOAD
	oldConfig := &CurdConfig{
		RofiSelection: false,
		MenuOrder:     "CURRENT,ALL,UNTRACKED,UPDATE,CONTINUE_LAST",
	}
	oldCategories := getOrderedCategories(oldConfig)
	foundInOld := false
	for _, cat := range oldCategories {
		if cat.Key == "DOWNLOAD" {
			foundInOld = true
			break
		}
	}
	if !foundInOld {
		t.Errorf("expected DOWNLOAD to be automatically appended for old configs in terminal menu")
	}
}

func TestGetOfflineCategories(t *testing.T) {
	// 1. Terminal mode: DOWNLOAD must be present as the first option
	termConfig := &CurdConfig{RofiSelection: false}
	cats := getOfflineCategories(termConfig)
	if len(cats) == 0 || cats[0].Key != "DOWNLOAD" {
		t.Fatalf("expected DOWNLOAD to be first offline category in terminal mode, got %v", cats)
	}

	keys := make(map[string]bool)
	for _, c := range cats {
		keys[c.Key] = true
	}
	for _, expected := range []string{"DOWNLOAD", "UNTRACKED", "CONTINUE_LAST", "PROVIDER", "RETRY"} {
		if !keys[expected] {
			t.Errorf("expected %s in offline categories, but missing", expected)
		}
	}

	// 2. Rofi mode: DOWNLOAD must be absent
	rofiConfig := &CurdConfig{RofiSelection: true}
	rofiCats := getOfflineCategories(rofiConfig)
	for _, c := range rofiCats {
		if c.Key == "DOWNLOAD" {
			t.Errorf("expected DOWNLOAD to be excluded in rofi offline menu")
		}
	}
}
