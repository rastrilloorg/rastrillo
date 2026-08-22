package scope

import (
	"testing"

	"github.com/carlosframework/rastrillo/gormlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type note struct {
	ID     int64
	UserID int64
}

func TestOwnedFiltersByUserID(t *testing.T) {
	db, err := gorm.Open(gormlite.Open("file:scope?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// AutoMigrate the test struct
	if err := db.AutoMigrate(&note{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Create rows for owners 1 and 2
	owner1Note := note{ID: 1, UserID: 1}
	owner2Note := note{ID: 2, UserID: 2}

	if err := db.Create(&owner1Note).Error; err != nil {
		t.Fatalf("failed to create owner 1 note: %v", err)
	}
	if err := db.Create(&owner2Note).Error; err != nil {
		t.Fatalf("failed to create owner 2 note: %v", err)
	}

	// Test 1: Owned(g, 2).First(&n, 1) with a note ID belonging to owner 1
	// should return gorm.ErrRecordNotFound (the 404-not-403 shape)
	var n note
	err = Owned(db, 2).First(&n, 1).Error
	if err != gorm.ErrRecordNotFound {
		t.Errorf("Expected gorm.ErrRecordNotFound, got %v", err)
	}

	// Test 2: Owned(g, 1).Find(&all) should return only owner 1's rows
	var all []note
	if err := Owned(db, 1).Find(&all).Error; err != nil {
		t.Fatalf("failed to find owner 1 notes: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("Expected 1 note for owner 1, got %d", len(all))
	}
	if all[0].UserID != 1 {
		t.Errorf("Expected UserID 1, got %d", all[0].UserID)
	}
}

func TestOwnedByCustomColumn(t *testing.T) {
	db, err := gorm.Open(gormlite.Open("file:scope?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	type team struct {
		ID     int64
		TeamID int64
	}

	// AutoMigrate the test struct
	if err := db.AutoMigrate(&team{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	// Create rows for teams 1 and 2
	team1Row := team{ID: 1, TeamID: 1}
	team2Row := team{ID: 2, TeamID: 2}

	if err := db.Create(&team1Row).Error; err != nil {
		t.Fatalf("failed to create team 1: %v", err)
	}
	if err := db.Create(&team2Row).Error; err != nil {
		t.Fatalf("failed to create team 2: %v", err)
	}

	// Test: OwnedBy(g, "team_id", 1).Find(&all) should return only team 1's rows
	var all []team
	if err := OwnedBy(db, "team_id", 1).Find(&all).Error; err != nil {
		t.Fatalf("failed to find team 1: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("Expected 1 team for team_id=1, got %d", len(all))
	}
	if all[0].TeamID != 1 {
		t.Errorf("Expected TeamID 1, got %d", all[0].TeamID)
	}
}

func TestOwnedByRejectsBadColumn(t *testing.T) {
	db, err := gorm.Open(gormlite.Open("file:scope?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	// Test: OwnedBy with a bad column should panic
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("Expected panic with bad column, but no panic occurred")
		}
	}()

	OwnedBy(db, `user_id = 1 OR ""=`, 1)
}
