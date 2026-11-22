package app

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRecordNoteView(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	history := &History{db: db}
	defer history.Close()
	mock.ExpectExec(`INSERT INTO note_views`).WithArgs("physics/量子力学/角动量").WillReturnResult(sqlmock.NewResult(0, 1))
	recorded, err := history.RecordNoteView("physics/量子力学/角动量")
	if err != nil || !recorded {
		t.Fatalf("record view: recorded=%t err=%v", recorded, err)
	}
	mock.ExpectExec(`INSERT INTO note_views`).WithArgs("missing").WillReturnResult(sqlmock.NewResult(0, 0))
	recorded, err = history.RecordNoteView("missing")
	if err != nil || recorded {
		t.Fatalf("draft or missing note: recorded=%t err=%v", recorded, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReadingOverview(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	history := &History{db: db}
	defer history.Close()
	now := time.Now()
	noteColumns := []string{"id", "slug", "title", "category", "category_title", "course", "course_slug", "views", "recent_views"}
	mock.ExpectQuery(`SELECT n.id, n.slug`).WithArgs(6).WillReturnRows(sqlmock.NewRows(noteColumns).
		AddRow("physics/量子力学/角动量", "jiao-dong-liang", "角动量", "physics", "物理", "量子力学", "quantum-mechanics", 12, 5))
	popular, err := history.PopularNotes(6)
	if err != nil || len(popular) != 1 || popular[0].Views != 12 || popular[0].RecentViews != 5 {
		t.Fatalf("popular: %+v, %v", popular, err)
	}

	mock.ExpectQuery(`SELECT COALESCE\(SUM\(views\), 0\)`).WillReturnRows(sqlmock.NewRows([]string{"total", "recent", "active"}).AddRow(42, 9, 3))
	mock.ExpectQuery(`SELECT n.id, n.slug`).WithArgs(8).WillReturnRows(sqlmock.NewRows(noteColumns).
		AddRow("physics/量子力学/角动量", "jiao-dong-liang", "角动量", "physics", "物理", "量子力学", "quantum-mechanics", 30, 7))
	mock.ExpectQuery(`SELECT n.category, n.category_title`).WillReturnRows(sqlmock.NewRows([]string{"category", "category_title", "course", "course_slug", "views"}).
		AddRow("physics", "物理", "量子力学", "quantum-mechanics", 30))
	mock.ExpectQuery(`SELECT viewed_on, SUM\(views\)`).WillReturnRows(sqlmock.NewRows([]string{"viewed_on", "views"}).AddRow(now, 9))
	overview, err := history.ReadingOverview()
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalViews != 42 || overview.RecentViews != 9 || overview.ActiveNotes != 3 || len(overview.PopularNotes) != 1 || len(overview.Courses) != 1 || len(overview.Daily) != 1 {
		t.Fatalf("overview: %+v", overview)
	}
	if overview.Courses[0].Views != 30 || overview.Daily[0].Views != 9 {
		t.Fatalf("overview values: %+v", overview)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
