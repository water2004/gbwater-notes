package app

import "time"

type PopularNote struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	Category      string `json:"category"`
	CategoryTitle string `json:"categoryTitle"`
	Course        string `json:"course"`
	CourseSlug    string `json:"courseSlug"`
	Views         int64  `json:"views"`
	RecentViews   int64  `json:"recentViews"`
}

type CourseViews struct {
	Category      string `json:"category"`
	CategoryTitle string `json:"categoryTitle"`
	Course        string `json:"course"`
	CourseSlug    string `json:"courseSlug"`
	Views         int64  `json:"views"`
}

type DailyViews struct {
	Date  time.Time `json:"date"`
	Views int64     `json:"views"`
}

type ReadingOverview struct {
	TotalViews   int64         `json:"totalViews"`
	RecentViews  int64         `json:"recentViews"`
	ActiveNotes  int64         `json:"activeNotes"`
	PopularNotes []PopularNote `json:"popularNotes"`
	Courses      []CourseViews `json:"courses"`
	Daily        []DailyViews  `json:"daily"`
}

func (h *History) RecordNoteView(noteID string) (bool, error) {
	result, err := h.db.Exec(`INSERT INTO note_views(note_id, viewed_on, views)
		SELECT id, CURRENT_DATE, 1 FROM notes WHERE id = $1 AND draft = FALSE
		ON CONFLICT (note_id, viewed_on) DO UPDATE SET views = note_views.views + 1`, noteID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func (h *History) PopularNotes(limit int) ([]PopularNote, error) {
	if limit <= 0 {
		limit = 6
	}
	if limit > 20 {
		limit = 20
	}
	rows, err := h.db.Query(`SELECT n.id, n.slug, n.title, n.category, n.category_title, n.course, n.course_slug,
		COALESCE(SUM(v.views), 0) AS views,
		COALESCE(SUM(v.views) FILTER (WHERE v.viewed_on >= CURRENT_DATE - 29), 0) AS recent_views
		FROM notes n LEFT JOIN note_views v ON v.note_id = n.id
		WHERE n.draft = FALSE GROUP BY n.id ORDER BY views DESC, n.title LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := []PopularNote{}
	for rows.Next() {
		var note PopularNote
		if err := rows.Scan(&note.ID, &note.Slug, &note.Title, &note.Category, &note.CategoryTitle, &note.Course, &note.CourseSlug, &note.Views, &note.RecentViews); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	return notes, rows.Err()
}

func (h *History) ReadingOverview() (ReadingOverview, error) {
	overview := ReadingOverview{PopularNotes: []PopularNote{}, Courses: []CourseViews{}, Daily: []DailyViews{}}
	if err := h.db.QueryRow(`SELECT COALESCE(SUM(views), 0),
		COALESCE(SUM(views) FILTER (WHERE viewed_on >= CURRENT_DATE - 6), 0),
		COUNT(DISTINCT note_id) FROM note_views`).Scan(&overview.TotalViews, &overview.RecentViews, &overview.ActiveNotes); err != nil {
		return overview, err
	}
	popular, err := h.PopularNotes(8)
	if err != nil {
		return overview, err
	}
	overview.PopularNotes = popular
	rows, err := h.db.Query(`SELECT n.category, n.category_title, n.course, n.course_slug, SUM(v.views) AS views
		FROM note_views v JOIN notes n ON n.id = v.note_id WHERE n.draft = FALSE
		GROUP BY n.category, n.category_title, n.course, n.course_slug ORDER BY views DESC, n.course LIMIT 6`)
	if err != nil {
		return overview, err
	}
	defer rows.Close()
	for rows.Next() {
		var course CourseViews
		if err := rows.Scan(&course.Category, &course.CategoryTitle, &course.Course, &course.CourseSlug, &course.Views); err != nil {
			return overview, err
		}
		overview.Courses = append(overview.Courses, course)
	}
	if err := rows.Err(); err != nil {
		return overview, err
	}
	if err := rows.Close(); err != nil {
		return overview, err
	}
	daily, err := h.db.Query(`SELECT viewed_on, SUM(views) FROM note_views
		WHERE viewed_on >= CURRENT_DATE - 13 GROUP BY viewed_on ORDER BY viewed_on`)
	if err != nil {
		return overview, err
	}
	defer daily.Close()
	for daily.Next() {
		var day DailyViews
		if err := daily.Scan(&day.Date, &day.Views); err != nil {
			return overview, err
		}
		overview.Daily = append(overview.Daily, day)
	}
	return overview, daily.Err()
}
