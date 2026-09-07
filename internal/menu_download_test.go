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
